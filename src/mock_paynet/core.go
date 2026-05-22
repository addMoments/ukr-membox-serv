package mockpaynet

import (
	"encoding/json"
	"fmt"
	"membox-serv/src/mycrypto"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"

	"github.com/gorilla/mux"
)

type Paylink_req struct {
	NameSurname            string  `json:"name_surname"`
	UserName               string  `json:"user_name"`
	Amount                 float64 `json:"amount"`
	Note                   string  `json:"note"`
	ReferenceNo            string  `json:"reference_no"`
	SucceedURL             string  `json:"succeed_url"`
	ErrorURL               string  `json:"error_url"`
	ConfirmationURL        string  `json:"confirmation_url"`
	Multi_payment          bool    `json:"multi_payment"`
	Addcomission_to_amount bool    `json:"addcomission_to_amount"`
}

type Paylink_res struct {
	URL        string `json:"url"`
	ID         string `json:"id"`
	ObjectName string `json:"object_name"`
	Code       int    `json:"code"`
	Message    string `json:"message"`
}

var root string

func guid() (uid int, err error) {
	return mycrypto.Rand_Int(1, 100000000)
}

func Init(root_folder string, r *mux.Router) {
	folder_st, err := os.Stat(root_folder)
	if err != nil {
		os.MkdirAll(root_folder, 0755)
		folder_st, err = os.Stat(root_folder)
		if err != nil {
			panic("mock paynet-2 " + err.Error())
		}
	}

	if !folder_st.IsDir() {
		os.MkdirAll(root_folder, 0755)
		panic("mock paynet-2 " + err.Error())
	}

	if !folder_st.IsDir() {
		panic("mock paynet-1 root folder not a dir")
	}

	root = root_folder

	r.HandleFunc("/api/mock-paylink/{uid}", mock_plink)
	// r.HandleFunc("/api/procplink/{type}/{uid}", proc_plink)

}

func load_plink(uid int) (r Paylink_req, err error) {
	byt, err := os.ReadFile(filepath.Join(root, fmt.Sprintf("%d.json", uid)))
	if err != nil {
		return
	}

	err = json.Unmarshal(byt, &r)
	return

}

func html_templ(body string, args ...interface{}) []byte {
	return []byte(fmt.Sprintf(`<!DOCTYPE html>
	<html lang="en">
	<head>
		<meta charset="UTF-8">
		<meta name="viewport" content="width=device-width, initial-scale=1.0">
		<title>Document</title>
	</head>
	<body>
		%s
	</body>
	</html>`, fmt.Sprintf(body, args...)))
}

func proc_plink(w http.ResponseWriter, r *http.Request) {
	var is_temp = true
	var redr_url string
	var err error
	var stat_code int

	defer (func() {
		if err != nil {
			if stat_code == 0 {
				stat_code = 500
			}
			http.Error(w, err.Error(), stat_code)
			return
		}

		if stat_code == 0 {
			if is_temp {
				stat_code = http.StatusTemporaryRedirect
			} else {
				stat_code = http.StatusPermanentRedirect
			}
		}

		http.Redirect(w, r, redr_url, stat_code)
		// w.WriteHeader(200)
		// w.Write([]byte(redr_url))
	})()

	fmt.Println("proc plik trig")

	uid_str, ok := mux.Vars(r)["uid"]
	if !ok {
		err = fmt.Errorf("uid not provided")
		return
	}

	proc_type, ok := mux.Vars(r)["type"]
	if !ok {
		err = fmt.Errorf("proc_type not provided")
		return
	}

	uid, err := strconv.Atoi(uid_str)
	if err != nil {
		return
	}

	fmt.Println(os.ReadDir(root))

	req_data, err := load_plink(uid)
	if err != nil {
		return
	}

	if proc_type == "ok" {
		redr_url = req_data.SucceedURL
	} else if proc_type == "err" {
		redr_url = req_data.ErrorURL
	} else {
		err = fmt.Errorf("invalid proc type: " + proc_type)
	}

	if err == nil {
		vals := url.Values{}
		vals.Add("xact_id", uid_str)

		redr_url += "?"
		redr_url += vals.Encode()

		err = os.Rename(filepath.Join(root, fmt.Sprintf("%d.json", uid)), filepath.Join(root, fmt.Sprintf("%s-%d.json", proc_type, uid)))
	}

}

func mock_plink(w http.ResponseWriter, r *http.Request) {
	var stat_code = 0
	var payload = []byte{}
	var err error

	defer (func() {
		if err != nil {
			if stat_code == 0 {
				stat_code = 500
			}
			http.Error(w, err.Error(), stat_code)
			return
		}
		if stat_code == 0 {
			stat_code = 200
		}
		w.WriteHeader(stat_code)
		w.Write(payload)
	})()

	uid_str, ok := mux.Vars(r)["uid"]
	if !ok {
		err = fmt.Errorf("uid not provided")
		return
	}

	uid, err := strconv.Atoi(uid_str)
	if err != nil {
		return
	}

	req_data, err := load_plink(uid)
	if err != nil {
		return
	}

	payload = html_templ(`
	<p>Tutar: %.2f</p>
	<p>
		<a href="%s"><button>Basarili islem</button></a>
		<a href="%s"><button>Basarisiz islem</button></a>
	</p>
	<p>Ham veri: %v</p>
	`, req_data.Amount, req_data.SucceedURL+"?id="+uid_str, req_data.ErrorURL+"?id="+uid_str, req_data)
}

func (p *Paylink_req) Do() (res_dt Paylink_res, err error) {
	uid, err := guid()
	if err != nil {
		return
	}

	byt, err := json.Marshal(p)
	if err != nil {
		return
	}

	err = os.WriteFile(filepath.Join(root, fmt.Sprintf("%d.json", uid)), byt, 0666)
	if err != nil {
		return
	}

	res_dt.ID = strconv.Itoa(uid)
	res_dt.Code = 0
	res_dt.Message = "Islem basarili"
	res_dt.ObjectName = "mock-paylink-" + res_dt.ID
	res_dt.URL = "/api/mock-paylink/" + res_dt.ID

	return res_dt, err
}
