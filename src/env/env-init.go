package env

import (
	"encoding/json"
	db "membox-serv/src/db_layer"
	s3wrap "membox-serv/src/s3-wrap"
	"os"
	"strconv"

	sendemail "membox-serv/src/send_email"
)

type env struct {
	ServRoot      string              `json:"serv_root"`
	Local_port    int                 `json:"local_port"`
	Db            db.DatabaseConfig   `json:"db"`
	Dev_key       string              `json:"dev_key"`
	Jwt_secret    string              `json:"jwt_secret"`
	S3            s3wrap.S3_init_inp  `json:"s3"`
	PaymentSecret string              `json:"payment_secret"`
	Smtp          sendemail.Mail_serv `json:"smtp"`
	Serv_name     string              `json:"server_unique_name"`
	AdminEmails   []string            `json:"admin_emails"`
	LiqPay        struct {
		PublicKey  string `json:"public_key"`
		PrivateKey string `json:"private_key"`
		Sandbox    bool   `json:"sandbox"`
	} `json:"liqpay"`
	NovaPoshta struct {
		APIKey          string `json:"api_key"`
		SenderRef       string `json:"sender_ref"`
		SenderContactRef string `json:"sender_contact_ref"`
		SenderAddressRef string `json:"sender_address_ref"`
		SenderCityRef   string `json:"sender_city_ref"`
		SenderPhone     string `json:"sender_phone"`
	} `json:"nova_poshta"`
}

var my_env env
var is_init = false
var is_live bool

func Env_init(islive bool) {
	is_live = islive

	env_byt, err := os.ReadFile(".env")
	if err != nil {
		panic(err)
	}

	err = json.Unmarshal(env_byt, &my_env)
	if err != nil {
		panic(err)
	}

	is_init = true
}

func Env() *env {
	if !is_init {
		panic("env is called but not init")
	}
	return &my_env
}

// without protocol
func Current_root() string {
	if is_live {
		return my_env.ServRoot
	}
	return "127.0.0.1:" + strconv.Itoa(my_env.Local_port)
}

func Is_live() bool {
	return is_live
}
