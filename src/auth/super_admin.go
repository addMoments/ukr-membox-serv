package auth

import (
	db "membox-serv/src/db_layer"
	"membox-serv/src/env"
	"net/http"

	sqlbuilder "github.com/huandu/go-sqlbuilder"
)

const (
	PanelRoleOrderAdmin = "order_admin"
	PanelRoleSuperAdmin = "super_admin"
)

// GetPanelAdminRole, kullanicinin aktif site panel rolunu okur.
// deleted_at dolu kayitlar revoke edilmis admin gecmisidir, aktif yetki sayilmaz.
func GetPanelAdminRole(userUID string) (string, error) {
	sb := sqlbuilder.BuildNamed(`
		SELECT COALESCE((
			SELECT role
			FROM panel_admins
			WHERE user_uid = ${user_uid}
			  AND deleted_at IS NULL
			LIMIT 1
		), '')
		FROM users
		WHERE uid = ${user_uid}
	`, map[string]interface{}{"user_uid": userUID})

	res, err := db.Query_one(sb)
	if err != nil {
		return "", err
	}
	return string(res[0]), nil
}

// IsPanelOrderAdmin, kullanicinin DB'de siparis paneli admini olup olmadigini kontrol eder.
// Bu rol sadece admin order ekranlari icindir; super admin yetkisi anlamina gelmez.
func IsPanelOrderAdmin(userUID string) (bool, error) {
	role, err := GetPanelAdminRole(userUID)
	if err != nil {
		return false, err
	}
	return role == PanelRoleOrderAdmin, nil
}

// IsPanelSuperAdmin, ileride super adminlerin de DB'den yonetilebilmesi icin hazirlanan kontroldur.
// Mevcut sistemde env.admin_emails kaynakli super admin davranisi korunur.
func IsPanelSuperAdmin(userUID string) (bool, error) {
	role, err := GetPanelAdminRole(userUID)
	if err != nil {
		return false, err
	}
	return role == PanelRoleSuperAdmin, nil
}

// IsSuperAdmin, once mevcut env.admin_emails listesini, sonra DB'deki panel super rolunu kontrol eder.
// Bu siralama mevcut calisan super admin sistemini korur; DB bosken davranis degismez.
func IsSuperAdmin(userUID string) (bool, error) {
	sb := sqlbuilder.NewSelectBuilder()
	sb.Select("mail").From("users").Where(sb.Equal("uid", userUID))
	res, err := db.Query_one(sb)
	if err != nil {
		return false, err
	}
	email := string(res[0])
	for _, adminEmail := range env.Env().AdminEmails {
		if adminEmail == email {
			return true, nil
		}
	}
	return IsPanelSuperAdmin(userUID)
}

func SuperAdminMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return AuthMiddleware(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := r.Context().Value("claims").(TokenClaims)
		if !ok {
			http.Error(w, "unauthorized", 401)
			return
		}
		isAdmin, err := IsSuperAdmin(claims.UserUID)
		if err != nil || !isAdmin {
			http.Error(w, "forbidden", 403)
			return
		}
		next.ServeHTTP(w, r)
	}, "auth")
}

// OrderPanelMiddleware, site siparis paneline erisebilecek kullanicilari kontrol eder.
// Super admin tam yetkiyle, order_admin ise sadece siparis route'lari icin kabul edilir.
func OrderPanelMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return AuthMiddleware(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := r.Context().Value("claims").(TokenClaims)
		if !ok {
			http.Error(w, "unauthorized", 401)
			return
		}

		isSuperAdmin, err := IsSuperAdmin(claims.UserUID)
		if err != nil {
			http.Error(w, "forbidden", 403)
			return
		}
		if isSuperAdmin {
			next.ServeHTTP(w, r)
			return
		}

		isOrderAdmin, err := IsPanelOrderAdmin(claims.UserUID)
		if err != nil || !isOrderAdmin {
			http.Error(w, "forbidden", 403)
			return
		}

		next.ServeHTTP(w, r)
	}, "auth")
}
