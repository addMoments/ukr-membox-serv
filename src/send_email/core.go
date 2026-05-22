package sendemail

import (
	"crypto/tls"
	"fmt"
	"io"
	"membox-serv/src/utils"
	"net/smtp"
	"strings"
	"sync"
)

type Mail_serv struct {
	Outgoing_server string `json:"outgoing_server"`
	Smtp_port       int    `json:"smtp_port"`
	Username        string `json:"username"`
	Password        string `json:"password"`
	Display_name    string `json:"display_name"`
	conn            *tls.Conn
	Client          *smtp.Client
	connMutex       sync.Mutex
}

var Info_mail *Mail_serv

func (m *Mail_serv) CheckConn() (err error) {
	m.connMutex.Lock()
	defer m.connMutex.Unlock()

	return m.checkConnNolock()

}

func (m *Mail_serv) checkConnNolock() (err error) {
	if m.Client == nil {
		return fmt.Errorf("client is nil")
	}
	err = m.Client.Noop()
	return
}

func (m *Mail_serv) Send(
	to []string,
	subject string,
	write_f func(io.WriteCloser),
	extra_headers map[string]string,
) (err error) {
	fmt.Printf("[email] sending to=%v subject=%q\n", to, subject)

	m.connMutex.Lock()
	defer m.connMutex.Unlock()

	connErr := m.checkConnNolock()
	if connErr != nil {
		fmt.Printf("[email] SMTP connection unhealthy (%v), reconnecting\n", connErr)
		err = m.init_smtp_nolock()
		if err != nil {
			fmt.Printf("[email] ERROR: reconnect failed: %v\n", err)
			return utils.Tag_err("se1", err)
		}
		fmt.Println("[email] reconnect OK")
	}

	from := m.Username

	m.Client.Reset()
	if err := m.Client.Mail(from); err != nil {
		fmt.Printf("[email] ERROR: set sender failed from=%s err=%v\n", from, err)
		return utils.Tag_err("se2", err)
	}

	for _, addr := range to {
		if err := m.Client.Rcpt(addr); err != nil {
			fmt.Printf("[email] ERROR: set recipient failed addr=%s err=%v\n", addr, err)
			return utils.Tag_err("se3", err)
		}
	}

	w, err := m.Client.Data()
	if err != nil {
		fmt.Printf("[email] ERROR: open data writer failed: %v\n", err)
		return utils.Tag_err("se4", err)
	}

	fmt.Fprint(w, mail_header("To", strings.Join(to, ",")))
	fmt.Fprint(w, mail_header("From", fmt.Sprintf("%s <%s>", m.Display_name, m.Username)))
	fmt.Fprint(w, mail_header("Subject", subject))
	fmt.Fprint(w, mail_header("Content-Type", "text/html; charset=\"UTF-8\""))

	for k, v := range extra_headers {
		fmt.Fprint(w, mail_header(k, v))
	}

	fmt.Fprint(w, "\r\n")
	write_f(w)
	fmt.Fprint(w, "\r\n")

	if err = w.Close(); err != nil {
		fmt.Printf("[email] ERROR: close data writer (SMTP rejected message) to=%v err=%v\n", to, err)
		return err
	}

	fmt.Printf("[email] OK sent to=%v subject=%q\n", to, subject)
	return nil
}

func mail_header(key, val string) string {
	return fmt.Sprintf("%s: %s\r\n", key, val)
}
