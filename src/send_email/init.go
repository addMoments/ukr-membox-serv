package sendemail

import (
	"crypto/tls"
	"fmt"
	"net/smtp"
)

func Init(
	info_mail_conf *Mail_serv,
) {
	Info_mail = info_mail_conf
	err := Info_mail.Init_smtp()
	if err != nil {
		panic(err)
	}
	fmt.Println("email is init!")
}

func (m *Mail_serv) init_smtp_nolock() error {
	if m.Client != nil {
		m.Client.Quit()
		m.Client = nil
	}

	if m.conn != nil {
		m.conn.Close()
		m.conn = nil
	}

	tlsConfig := &tls.Config{
		InsecureSkipVerify: false,
		ServerName:         m.Outgoing_server,
		MinVersion:         tls.VersionTLS12,
	}

	var err error
	m.conn, err = tls.Dial("tcp", fmt.Sprintf("%s:%d", m.Outgoing_server, m.Smtp_port), tlsConfig)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}

	m.Client, err = smtp.NewClient(m.conn, m.Outgoing_server)
	if err != nil {
		m.conn.Close()
		return fmt.Errorf("failed to create SMTP client: %w", err)
	}

	if err := m.Client.Auth(smtp.PlainAuth("", m.Username, m.Password, m.Outgoing_server)); err != nil {
		m.Client.Quit()
		m.conn.Close()
		return fmt.Errorf("authentication failed: %w", err)
	}

	return nil

}

func (m *Mail_serv) Init_smtp() (err error) {
	m.connMutex.Lock()
	defer m.connMutex.Unlock()

	err = m.init_smtp_nolock()
	return

}
