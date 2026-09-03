package sendemail

import (
	"bytes"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"html"
	"io"
	"membox-serv/src/utils"
	"mime"
	"net/smtp"
	"regexp"
	"strings"
	"sync"
	"time"
)

type Mail_serv struct {
	Outgoing_server string `json:"outgoing_server"`
	Smtp_port       int    `json:"smtp_port"`
	Username        string `json:"username"`
	Password        string `json:"password"`
	Display_name    string `json:"display_name"`
	sendMutex       sync.Mutex
}

var Info_mail *Mail_serv

// Ne: Her gonderim icin yeni bir SMTP baglantisi acar (implicit TLS + PLAIN auth).
// Nasil: Baglanti cagirana dondurulur, cagiran kapatmakla yukumludur.
// Neden: Eskiden tek bir baglanti global olarak saklaniyordu ama pratikte hep olu
//
//	geliyordu: canli logda 30 gonderimin 30'unda once "broken pipe" gorulup
//	yeniden baglanildi. Havuz hicbir fayda saglamayip yalnizca fazladan bir tur
//	ve bir yaris penceresi ekliyordu.
func (m *Mail_serv) dial() (*smtp.Client, error) {
	tlsConfig := &tls.Config{
		InsecureSkipVerify: false,
		ServerName:         m.Outgoing_server,
		MinVersion:         tls.VersionTLS12,
	}

	conn, err := tls.Dial("tcp", fmt.Sprintf("%s:%d", m.Outgoing_server, m.Smtp_port), tlsConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to connect: %w", err)
	}

	client, err := smtp.NewClient(conn, m.Outgoing_server)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to create SMTP client: %w", err)
	}

	if err := client.Auth(smtp.PlainAuth("", m.Username, m.Password, m.Outgoing_server)); err != nil {
		client.Close()
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	return client, nil
}

func (m *Mail_serv) Send(
	to []string,
	subject string,
	write_f func(io.WriteCloser),
	extra_headers map[string]string,
) (err error) {
	fmt.Printf("[email] sending to=%v subject=%q\n", to, subject)

	// Govde once bellege yazilir: hem duz metin alternatifi ondan uretilecek,
	// hem de baglanti acilmadan once govdenin hazir olmasi DATA suresini kisaltir.
	var htmlBody bytes.Buffer
	write_f(nopWriteCloser{Writer: &htmlBody})

	m.sendMutex.Lock()
	defer m.sendMutex.Unlock()

	client, err := m.dial()
	if err != nil {
		fmt.Printf("[email] ERROR: connect failed: %v\n", err)
		return utils.Tag_err("se1", err)
	}
	defer client.Close()

	from := m.Username

	if err := client.Mail(from); err != nil {
		fmt.Printf("[email] ERROR: set sender failed from=%s err=%v\n", from, err)
		return utils.Tag_err("se2", err)
	}

	for _, addr := range to {
		if err := client.Rcpt(addr); err != nil {
			fmt.Printf("[email] ERROR: set recipient failed addr=%s err=%v\n", addr, err)
			return utils.Tag_err("se3", err)
		}
	}

	w, err := client.Data()
	if err != nil {
		fmt.Printf("[email] ERROR: open data writer failed: %v\n", err)
		return utils.Tag_err("se4", err)
	}

	if _, err = w.Write(m.build_message(to, subject, htmlBody.Bytes(), extra_headers)); err != nil {
		fmt.Printf("[email] ERROR: write message failed to=%v err=%v\n", to, err)
		w.Close()
		return utils.Tag_err("se5", err)
	}

	if err = w.Close(); err != nil {
		fmt.Printf("[email] ERROR: close data writer (SMTP rejected message) to=%v err=%v\n", to, err)
		return err
	}

	client.Quit()

	fmt.Printf("[email] OK sent to=%v subject=%q\n", to, subject)
	return nil
}

// Ne: Tam RFC 5322 mesajini uretir -- basliklar + multipart/alternative govde.
// Nasil: Iki parca (duz metin ve HTML) base64 ile kodlanir.
// Neden: Date/Message-ID/MIME-Version eksikligi ve tek parcali HTML, alici tarafinda
//
//	spam puanini yukselten klasik isaretler. base64 ayrica SMTP'nin 998 karakterlik
//	satir sinirini tamamen devre disi birakir -- sablondaki buton stili tek satirda
//	uzun bir dize.
func (m *Mail_serv) build_message(
	to []string,
	subject string,
	htmlBody []byte,
	extra_headers map[string]string,
) []byte {
	boundary := random_token(24)

	var msg bytes.Buffer
	write_mail_header(&msg, "To", strings.Join(to, ", "))
	write_mail_header(&msg, "From", fmt.Sprintf("%s <%s>", encode_header(m.Display_name), m.Username))
	write_mail_header(&msg, "Subject", encode_header(subject))
	write_mail_header(&msg, "Date", time.Now().Format(time.RFC1123Z))
	write_mail_header(&msg, "Message-ID", fmt.Sprintf("<%s@%s>", random_token(16), m.sender_domain()))
	write_mail_header(&msg, "MIME-Version", "1.0")
	write_mail_header(&msg, "Content-Type", `multipart/alternative; boundary="`+boundary+`"`)

	for k, v := range extra_headers {
		write_mail_header(&msg, k, v)
	}

	msg.WriteString("\r\n")

	msg.WriteString("--" + boundary + "\r\n")
	write_mail_header(&msg, "Content-Type", `text/plain; charset="UTF-8"`)
	write_mail_header(&msg, "Content-Transfer-Encoding", "base64")
	msg.WriteString("\r\n")
	msg.Write(base64_lines(html_to_text(htmlBody)))

	msg.WriteString("--" + boundary + "\r\n")
	write_mail_header(&msg, "Content-Type", `text/html; charset="UTF-8"`)
	write_mail_header(&msg, "Content-Transfer-Encoding", "base64")
	msg.WriteString("\r\n")
	msg.Write(base64_lines(htmlBody))

	msg.WriteString("--" + boundary + "--\r\n")

	return msg.Bytes()
}

func (m *Mail_serv) sender_domain() string {
	if i := strings.LastIndex(m.Username, "@"); i >= 0 && i+1 < len(m.Username) {
		return m.Username[i+1:]
	}
	return m.Outgoing_server
}

// Salt ASCII basliklari oldugu gibi birakir, degilse RFC 2047 ile kodlar.
func encode_header(val string) string {
	return mime.QEncoding.Encode("utf-8", val)
}

func write_mail_header(w io.Writer, key, val string) {
	fmt.Fprintf(w, "%s: %s\r\n", key, val)
}

func base64_lines(body []byte) []byte {
	encoded := base64.StdEncoding.EncodeToString(body)

	var out bytes.Buffer
	for len(encoded) > 76 {
		out.WriteString(encoded[:76])
		out.WriteString("\r\n")
		encoded = encoded[76:]
	}
	if len(encoded) > 0 {
		out.WriteString(encoded)
		out.WriteString("\r\n")
	}

	return out.Bytes()
}

func random_token(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

type nopWriteCloser struct {
	io.Writer
}

func (nopWriteCloser) Close() error { return nil }

var (
	re_hidden_block = regexp.MustCompile(`(?is)<script\b[^>]*>.*?</script\s*>|<style\b[^>]*>.*?</style\s*>|<head\b[^>]*>.*?</head\s*>`)
	re_anchor       = regexp.MustCompile(`(?is)<a\b[^>]*\bhref\s*=\s*"([^"]*)"[^>]*>(.*?)</\s*a\s*>`)
	re_line_break   = regexp.MustCompile(`(?i)<br\s*/?>|</\s*(p|div|tr|td|table|h[1-6])\s*>`)
	re_any_tag      = regexp.MustCompile(`(?s)<[^>]*>`)
	re_inline_space = regexp.MustCompile(`[ \t]+`)
	re_blank_lines  = regexp.MustCompile(`\n{3,}`)
)

// Ne: HTML govdeden okunabilir bir duz metin alternatifi uretir.
// Nasil: Linkler "etiket (adres)" haline gelir, blok kapanislari satir sonuna cevrilir,
//
//	kalan etiketler atilir ve bosluklar toparlanir.
//
// Neden: Cagri yerleri govdeyi HTML olarak yaziyor; ayri bir duz metin sablonu tutmak
//
//	iki metnin zamanla birbirinden ayrilmasi demek olurdu.
func html_to_text(src []byte) []byte {
	s := string(src)
	s = re_hidden_block.ReplaceAllString(s, "")
	s = re_anchor.ReplaceAllString(s, "$2 ($1)")
	s = re_line_break.ReplaceAllString(s, "\n")
	s = re_any_tag.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	s = re_inline_space.ReplaceAllString(s, " ")

	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSpace(line)
	}
	s = strings.Join(lines, "\n")
	s = re_blank_lines.ReplaceAllString(s, "\n\n")

	return []byte(strings.TrimSpace(s) + "\n")
}
