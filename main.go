package main

import (
	"fmt"
	"membox-serv/src/auth"
	db "membox-serv/src/db_layer"
	dbscripts "membox-serv/src/db_scripts"
	"membox-serv/src/env"
	liqpayprovider "membox-serv/src/liqpay"
	mockpaynet "membox-serv/src/mock_paynet"
	"membox-serv/src/mycrypto"
	"membox-serv/src/payments"
	promocron "membox-serv/src/promo_cron"
	"membox-serv/src/routes"
	s3wrap "membox-serv/src/s3-wrap"
	"membox-serv/src/types"
	"membox-serv/src/worker"
	"net/http"
	"os"
	"strconv"

	sendemail "membox-serv/src/send_email"
	storagecron "membox-serv/src/storage_cron"

	"github.com/gorilla/mux"
	"github.com/rs/cors"
)

var is_live = true

const cert_file = "/etc/letsencrypt/live/serv.addmoments.com.ua/fullchain.pem"
const key_file = "/etc/letsencrypt/live/serv.addmoments.com.ua/privkey.pem"

func init() {
	if len(os.Args) < 2 || os.Args[1] != "true" {
		is_live = false
	}

	if is_live {
		env.Write_pid()
	}

	env.Env_init(is_live)
	s3wrap.Public_s3.Init(env.Env().S3)
	e := env.Env()
	db.Init(e.Db)
	sendemail.Init(&e.Smtp)
	storagecron.Init()
	promocron.Init()
}

func maiasdn() {
	fmt.Println(mycrypto.Decrypt("eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJleHAiOjE3NzIwODE1OTksImlhdCI6MTc3MTQ3Njc5OSwiaXAiOiIxMjcuMC4wLjEiLCJyb2xlIjoiYXV0aCIsInVpIjoiODA3YmZlMGQtYWI2My00MTZhLWJmOWItMDc3YjI0Y2JiNTg1In0.Gwu37qsILlmToxW6V9yLpk-eMVqPd5BaJUjbr5wyqf0", []byte(env.Env().PaymentSecret)))
}

func madfin() {
	eventUID := "07935736-3501-41c4-aaa6-eb0aaf464cc3"

	tier, err := dbscripts.Event_tier(eventUID)
	if err != nil {
		fmt.Println("Event_tier error:", err)
	} else {
		fmt.Println("Event_tier:", tier)
	}

	mediaCount, err := dbscripts.Event_limit(eventUID, "storage_days")
	if err != nil {
		fmt.Println("Event_limit error:", err)
	} else {
		fmt.Println("media_count:", mediaCount)
	}
}

func main() {
	var err error
	r := mux.NewRouter().StrictSlash(false)

	protocol := "http"
	if is_live {
		protocol = "https"
	}

	// Initialize payment providers
	e := env.Env()
	err = payments.Init(
		map[string]types.Js_object{
			"mock_paynet": {
				"root_folder": "./mockpnet",
				"router":      r,
			},
			"liqpay": {
				"public_key":  e.LiqPay.PublicKey,
				"private_key": e.LiqPay.PrivateKey,
				"sandbox":     e.LiqPay.Sandbox,
			},
		},
		map[string]payments.PaymentProvider{
			"mock_paynet": mockpaynet.NewProvider(),
			"liqpay":      liqpayprovider.NewProvider(),
		},
		e.PaymentSecret,
		protocol+"://"+env.Current_root(),
		r,
		routes.PaymentCBRoutes.Payment_cb,
	)
	if err != nil {
		fmt.Println("Failed to initialize payments:", err)
		os.Exit(1)
	}

	s3_export_worker := worker.Worker{
		Job_name:  "s3_export",
		Serv_name: env.Env().Serv_name,
		Job_func:  routes.Export_s3,
	}

	err = s3_export_worker.Init(2)
	if err != nil {
		fmt.Println("Failed to initialize s3 export worker:", err)
		os.Exit(1)
	}

	go (func() {
		env.Env().Db.ListenForNotifications("job_insert", func(data string) {
			s3_export_worker.Kick()
			fmt.Println("job insert detected", data)
		})
	})()

	authRoutes := r.PathPrefix("/auth").Subrouter()
	authRoutes.HandleFunc("/signin/email", routes.AuthRoutes.SigninPassword)
	authRoutes.HandleFunc("/whoami", auth.AuthMiddleware(routes.AuthRoutes.WhoAmI, "auth"))
	authRoutes.HandleFunc("/account", auth.AuthMiddleware(routes.AuthRoutes.DeleteAccount, "auth")).Methods("DELETE")

	apiRoutes := r.PathPrefix("/api").Subrouter()
	apiRoutes.HandleFunc("/upload/{purpose}", auth.AuthMiddleware(routes.UploadRoutes.Upload, "auth"))
	apiRoutes.HandleFunc("/auth/upload/{uploadPackedUid}", auth.AuthMiddleware(routes.UploadRoutes.Delete, "auth")).Methods("DELETE")
	apiRoutes.HandleFunc("/qr/{eventPackedUid}", auth.AuthMiddleware(routes.Adjust_event_qr, "auth"))
	apiRoutes.HandleFunc("/calc-size/{eventPackedUid}", auth.AuthMiddleware(routes.Calc_size_route, "auth"))
	apiRoutes.HandleFunc("/products", routes.ProductRoutes.GetProducts).Methods("GET")
	apiRoutes.HandleFunc("/promo/validate", routes.PromoRoutes.Validate).Methods("POST")
	apiRoutes.HandleFunc("/admin/products", auth.SuperAdminMiddleware(routes.ProductRoutes.AdminListProducts)).Methods("GET")
	apiRoutes.HandleFunc("/admin/products/upload-url", auth.SuperAdminMiddleware(routes.ProductRoutes.AdminCreateAddonImageUploadURL)).Methods("POST")
	apiRoutes.HandleFunc("/admin/products/{productUid}", auth.SuperAdminMiddleware(routes.ProductRoutes.AdminUpdateProduct)).Methods("PATCH")
	// T-14 uyari mail + T-0 soft-delete'i manuel tetikler. ?dry_run=true ile
	// hicbir veriyi degistirmeden sadece etkilenecek sayilari raporlar.
	apiRoutes.HandleFunc("/admin/run-storage-check", auth.AuthMiddleware(routes.AdminStorageRoutes.RunStorageCheck, "auth")).Methods("POST")
	apiRoutes.HandleFunc("/event/{eventPackedUid}/features", auth.AuthMiddleware(routes.FeaturesRoute.Private, "auth")).Methods("GET")
	apiRoutes.HandleFunc("/event/{eventPackedUid}/public-features", routes.FeaturesRoute.Public).Methods("GET")
	apiRoutes.HandleFunc("/event/{eventPackedUid}/advertorial", auth.AuthMiddleware(routes.AdvertorialRoutes.PrivateGet, "auth")).Methods("GET")
	apiRoutes.HandleFunc("/event/{eventPackedUid}/advertorial", auth.AuthMiddleware(routes.AdvertorialRoutes.PrivatePatch, "auth")).Methods("PATCH")
	apiRoutes.HandleFunc("/event/{eventPackedUid}/advertorial/upload-url", auth.AuthMiddleware(routes.AdvertorialRoutes.PrivateUploadURL, "auth")).Methods("POST")
	apiRoutes.HandleFunc("/event/{eventPackedUid}/advertorial-public", routes.AdvertorialRoutes.PublicGet).Methods("GET")
	apiRoutes.HandleFunc("/event/{eventPackedUid}/stats", auth.AuthMiddleware(routes.EventRoutes.Stats, "auth")).Methods("GET")
	// Storage uzatma: tek seferlik hak. Body'siz POST; iz kuralini handler yurutuyor.
	apiRoutes.HandleFunc("/event/{eventPackedUid}/extend-storage", auth.AuthMiddleware(routes.EventRoutes.ExtendStorage, "auth")).Methods("POST")

	apiRoutes.HandleFunc("/purchase", routes.ProductRoutes.Purchase)
	apiRoutes.HandleFunc("/purchase/{encPackedUID}/status", routes.ProductRoutes.PurchaseStatus).Methods("GET")
	if !is_live {
		apiRoutes.HandleFunc("/purchase/{encPackedUID}/simulate-success", routes.ProductRoutes.SimulatePaymentSuccess).Methods("POST")
	}

	// Nova Poshta proxy (no auth required — API key stays server-side)
	apiRoutes.HandleFunc("/np/settlements", routes.NPProxyRoutes.Settlements).Methods("GET")
	apiRoutes.HandleFunc("/np/warehouses", routes.NPProxyRoutes.Warehouses).Methods("GET")

	apiRoutes.HandleFunc("/download", routes.DownloadProxy).Methods("GET")
	apiRoutes.HandleFunc("/form/{formName}", routes.Form_route)

	// Order management — register specific paths before parameterized ones
	apiRoutes.HandleFunc("/admin/check", routes.OrderRoutes.AdminCheck).Methods("GET")
	apiRoutes.HandleFunc("/admin/panel-admins", auth.SuperAdminMiddleware(routes.PanelAdminRoutes.List)).Methods("GET")
	apiRoutes.HandleFunc("/admin/panel-admins", auth.SuperAdminMiddleware(routes.PanelAdminRoutes.Create)).Methods("POST")
	apiRoutes.HandleFunc("/admin/panel-admins/{userUID}", auth.SuperAdminMiddleware(routes.PanelAdminRoutes.Delete)).Methods("DELETE")
	apiRoutes.HandleFunc("/admin/partnerships", auth.SuperAdminMiddleware(routes.AdminPartnershipRoutes.List)).Methods("GET")
	apiRoutes.HandleFunc("/admin/partnerships", auth.SuperAdminMiddleware(routes.AdminPartnershipRoutes.Create)).Methods("POST")
	apiRoutes.HandleFunc("/admin/partnerships/{partnershipUID}", auth.SuperAdminMiddleware(routes.AdminPartnershipRoutes.Get)).Methods("GET")
	apiRoutes.HandleFunc("/admin/partnerships/{partnershipUID}", auth.SuperAdminMiddleware(routes.AdminPartnershipRoutes.Update)).Methods("PATCH")
	apiRoutes.HandleFunc("/admin/partnerships/{partnershipUID}", auth.SuperAdminMiddleware(routes.AdminPartnershipRoutes.Delete)).Methods("DELETE")
	apiRoutes.HandleFunc("/admin/promos", auth.SuperAdminMiddleware(routes.AdminPromoRoutes.List)).Methods("GET")
	apiRoutes.HandleFunc("/admin/promos", auth.SuperAdminMiddleware(routes.AdminPromoRoutes.Create)).Methods("POST")
	apiRoutes.HandleFunc("/admin/promos/report", auth.SuperAdminMiddleware(routes.AdminPromoRoutes.Report)).Methods("GET")
	apiRoutes.HandleFunc("/admin/promos/{promoUID}", auth.SuperAdminMiddleware(routes.AdminPromoRoutes.Update)).Methods("PATCH")
	apiRoutes.HandleFunc("/admin/promos/{promoUID}/enable", auth.SuperAdminMiddleware(routes.AdminPromoRoutes.Enable)).Methods("POST")
	apiRoutes.HandleFunc("/admin/promos/{promoUID}/disable", auth.SuperAdminMiddleware(routes.AdminPromoRoutes.Disable)).Methods("POST")
	apiRoutes.HandleFunc("/admin/promos/{promoUID}", auth.SuperAdminMiddleware(routes.AdminPromoRoutes.Delete)).Methods("DELETE")
	apiRoutes.HandleFunc("/admin/orders/items/{cartItemUID}/retry-waybill", auth.SuperAdminMiddleware(routes.OrderRoutes.RetryWaybill)).Methods("POST")
	apiRoutes.HandleFunc("/admin/orders/items/{cartItemUID}", auth.SuperAdminMiddleware(routes.OrderRoutes.UpdateItem)).Methods("PATCH")
	apiRoutes.HandleFunc("/admin/orders/{purchaseUID}", auth.OrderPanelMiddleware(routes.OrderRoutes.GetOrder)).Methods("GET")
	apiRoutes.HandleFunc("/admin/orders", auth.OrderPanelMiddleware(routes.OrderRoutes.ListOrders)).Methods("GET")
	apiRoutes.HandleFunc("/event/{eventPackedUID}/order/items/{cartItemUID}", auth.AuthMiddleware(routes.OrderRoutes.SubmitBuyerConfig, "auth")).Methods("PATCH")
	apiRoutes.HandleFunc("/event/{eventPackedUID}/order", auth.AuthMiddleware(routes.OrderRoutes.GetMyOrder, "auth")).Methods("GET")

	guestRoutes := apiRoutes.PathPrefix("/guest").Subrouter()
	guestRoutes.HandleFunc("/upload/{eventPackedUid}/{utype}", auth.AuthMiddleware(routes.UploadRoutes.GuestUpload, "webanon"))
	guestRoutes.HandleFunc("/whoami", auth.AuthMiddleware(routes.AuthRoutes.WhoAmI, "webanon"))

	// Signup email endpoints (public - token is the auth)
	authRoutes.HandleFunc("/signup/email/{token}", routes.SignupEmailRoutes.Get).Methods("GET")
	authRoutes.HandleFunc("/signup/email/{token}", routes.SignupEmailRoutes.Post).Methods("POST")
	authRoutes.HandleFunc("/signup/email/{token}/attach", routes.SignupEmailRoutes.Attach).Methods("POST")

	authRoutes.HandleFunc("/password-reset/request", routes.PasswordResetRoutes.Request).Methods("POST")
	authRoutes.HandleFunc("/password-reset/confirm", routes.PasswordResetRoutes.Confirm).Methods("POST")

	authRoutes.HandleFunc("/collaborator/{packedEventUid}", auth.AuthMiddleware(routes.CollaboratorRoutes.New, "auth")).Methods("POST")
	authRoutes.HandleFunc("/collaborator/{packedEventUid}/{packedCollaboratorUid}", auth.AuthMiddleware(routes.CollaboratorRoutes.DeleteByEvent, "auth")).Methods("DELETE")
	authRoutes.HandleFunc("/collaborator/{packedCollaboratorUid}", auth.AuthMiddleware(routes.CollaboratorRoutes.Delete, "auth")).Methods("DELETE")
	authRoutes.HandleFunc("/event/{eventPackedUid}", auth.AuthMiddleware(routes.EventRoutes.Delete, "auth")).Methods("DELETE")

	//authRoutes.HandleFunc("/collaborator/new", auth.AuthMiddleware(routes.CollaboratorRoutes.New, "auth"))

	r.HandleFunc("/l/{path:.*}", routes.LinkHandler.HandleLink)

	r.HandleFunc("/ui/{path:.*}", func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		path := vars["path"]
		http.Redirect(w, r, "https://memboxpub-qo1gff2e.s3.eu-north-1.amazonaws.com/ui/"+path, http.StatusFound)
	})

	//apiRoutes := r.PathPrefix("/api").Subrouter()
	//apiRoutes.HandleFunc("/qr", routes.QrRoutes.GetQr)

	// authRoutes.HandleFunc("/signin/ e", routes.AuthRoutes.SigninPassword)
	// authRoutes.HandleFunc("/signup/google", routes.AuthRoutes.SigninPassword)

	// r.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
	// 	r.URL.Path = "/home"
	// 	wordpressProxy.ServeHTTP(w, r)
	// })

	// Catch-all handler for React app (serves as fallback for all non-API routes)
	r.NotFoundHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !is_live {
			http.Redirect(w, r, "http://localhost:3000"+r.URL.RequestURI(), http.StatusSeeOther)
			return
		}

		http.Redirect(w, r, "https://addmoments.com.ua"+r.URL.RequestURI(), http.StatusSeeOther)
	})

	// CORS configuration with allowlist
	/*allowedOrigins := []string{
		// DEV origins
		"http://localhost:3000",
		"http://localhost:5173",
		"http://127.0.0.1:3000",
		"http://127.0.0.1:5173",
		// LIVE origins
		"https://chatdrop.devasilli.com",
		"https://app.devasilli.com",
		"https://www.devasilli.com",
	}*/

	c := cors.New(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "PATCH"},
		AllowedHeaders:   []string{"*"},
		ExposedHeaders:   []string{"X-Auth-Token", "Link"},
		AllowCredentials: true,
	})
	handler := c.Handler(r)
	if is_live {

		go (func() {
			redirectHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				target := "https://" + r.Host + r.URL.RequestURI()
				http.Redirect(w, r, target, http.StatusMovedPermanently)
			})
			err := http.ListenAndServe(":80", redirectHandler)
			if err != nil {
				fmt.Println("listen failed", err)
				os.Exit(1)
			}
		})()

		fmt.Println("live on addmoments.com.ua")
		err = http.ListenAndServeTLS(":443", cert_file, key_file, handler)

		if err != nil {
			fmt.Println("listen failed", err)
			os.Exit(1)
		}

		return
	}

	local_port := env.Env().Local_port
	fmt.Println("live on http://127.0.0.1:" + strconv.Itoa(local_port))
	err = http.ListenAndServe(":"+strconv.Itoa(local_port), handler)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

}
