package sendemail

import (
	"fmt"
)

func Init(
	info_mail_conf *Mail_serv,
) {
	Info_mail = info_mail_conf
	if Info_mail.Outgoing_server == "" {
		fmt.Println("smtp not configured, email disabled")
		return
	}

	// Acilista yalnizca ayarlarin dogrulugu sinanir; baglanti saklanmaz, her gonderim
	// kendi baglantisini acar.
	client, err := Info_mail.dial()
	if err != nil {
		panic(err)
	}
	client.Quit()

	fmt.Println("email is init!")
}
