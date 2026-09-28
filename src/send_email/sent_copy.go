package sendemail

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

const (
	imap_port    = 993
	imap_timeout = 30 * time.Second
	// Sunucudan gelen literal'ler yalnizca klasor adlari; bundan buyugu bozuk yanittir.
	imap_max_literal = 64 * 1024
)

// Ne: Gonderilmis bir mesajin birebir kopyasini noreply kutusunun Gonderilenler klasorune koyar.
// Nasil: SMTP ile ayni sunucu ve hesapla IMAP'e (993, implicit TLS) girer, \Sent isaretli
//
//	klasoru LIST'ten bulur ve mesaji okunmus olarak APPEND eder.
//
// Neden: Mesaji sunucu SMTP ile gonderiyor; bir mail programi gibi Gonderilenler'e kendisi
//
//	kopya koymuyor. Musteri aktivasyon maillerini orada gormek istedi (madde 2.2).
func (m *Mail_serv) save_sent_copy(raw []byte) error {
	dialer := &net.Dialer{Timeout: imap_timeout}
	conn, err := tls.DialWithDialer(dialer, "tcp", fmt.Sprintf("%s:%d", m.Outgoing_server, imap_port), &tls.Config{
		ServerName: m.Outgoing_server,
		MinVersion: tls.VersionTLS12,
	})
	if err != nil {
		return fmt.Errorf("imap connect: %w", err)
	}
	defer conn.Close()

	// Takilan bir sunucu goroutine'i sonsuza kadar tutmasin.
	conn.SetDeadline(time.Now().Add(imap_timeout))

	return imap_append_to_sent(conn, m.Username, m.Password, raw)
}

type imap_literal []byte

type imap_session struct {
	w    io.Writer
	r    *bufio.Reader
	next int
}

func imap_append_to_sent(conn io.ReadWriter, user, pass string, raw []byte) error {
	s := &imap_session{w: conn, r: bufio.NewReader(conn)}

	greeting, err := s.read_line()
	if err != nil {
		return fmt.Errorf("imap greeting: %w", err)
	}
	if !strings.HasPrefix(greeting, "* OK") {
		return fmt.Errorf("imap greeting: %q", greeting)
	}

	if _, err := s.command("LOGIN", imap_astring(user), imap_astring(pass)); err != nil {
		return fmt.Errorf("imap login: %w", err)
	}
	defer s.command("LOGOUT")

	lines, err := s.command(`LIST "" "*"`)
	if err != nil {
		return fmt.Errorf("imap list: %w", err)
	}
	folder := imap_sent_folder(lines)
	if folder == "" {
		return fmt.Errorf(`imap: no folder is marked \Sent`)
	}

	if _, err := s.command("APPEND", imap_quote(folder), `(\Seen)`, imap_literal(raw)); err != nil {
		return fmt.Errorf("imap append to %s: %w", folder, err)
	}

	return nil
}

// Ne: Tek bir komutu gonderir ve etiketli yaniti bekler; etiketsiz satirlari dondurur.
// Nasil: string parcalar oldugu gibi yazilir; imap_literal parcalarda once {n} gonderilir,
//
//	sunucunun "+" devam isareti beklenir, sonra baytlar yazilir.
func (s *imap_session) command(parts ...any) ([]string, error) {
	s.next++
	tag := "a" + strconv.Itoa(s.next)

	buf := []byte(tag)
	for _, p := range parts {
		buf = append(buf, ' ')
		switch v := p.(type) {
		case string:
			buf = append(buf, v...)
		case imap_literal:
			buf = append(buf, fmt.Sprintf("{%d}\r\n", len(v))...)
			if _, err := s.w.Write(buf); err != nil {
				return nil, err
			}
			buf = buf[:0]
			if err := s.wait_continuation(tag); err != nil {
				return nil, err
			}
			buf = append(buf, v...)
		}
	}
	buf = append(buf, "\r\n"...)
	if _, err := s.w.Write(buf); err != nil {
		return nil, err
	}

	var untagged []string
	for {
		line, err := s.read_line()
		if err != nil {
			return untagged, err
		}
		if rest, ok := strings.CutPrefix(line, tag+" "); ok {
			if strings.HasPrefix(rest, "OK") {
				return untagged, nil
			}
			return untagged, fmt.Errorf("%s", rest)
		}
		untagged = append(untagged, line)
	}
}

func (s *imap_session) wait_continuation(tag string) error {
	for {
		line, err := s.read_line()
		if err != nil {
			return err
		}
		if strings.HasPrefix(line, "+") {
			return nil
		}
		if rest, ok := strings.CutPrefix(line, tag+" "); ok {
			return fmt.Errorf("%s", rest)
		}
	}
}

// Ne: Sunucudan bir mantiksal satir okur, CRLF'siz dondurur.
// Nasil: Satir {n} ile bitiyorsa arkasindan gelen n bayt okunur ve tirnakli dize olarak
//
//	satira eklenir; boylece klasor adi literal gelse de ayristirici tek bicim gorur.
func (s *imap_session) read_line() (string, error) {
	var sb strings.Builder
	for {
		part, err := s.r.ReadString('\n')
		if err != nil {
			return "", err
		}
		part = strings.TrimRight(part, "\r\n")

		n, head, ok := imap_trailing_literal(part)
		if !ok {
			sb.WriteString(part)
			return sb.String(), nil
		}
		if n > imap_max_literal {
			return "", fmt.Errorf("imap literal too large: %d", n)
		}
		lit := make([]byte, n)
		if _, err := io.ReadFull(s.r, lit); err != nil {
			return "", err
		}
		sb.WriteString(head)
		sb.WriteString(imap_quote(string(lit)))
	}
}

func imap_trailing_literal(line string) (n int, head string, ok bool) {
	if !strings.HasSuffix(line, "}") {
		return 0, "", false
	}
	open := strings.LastIndexByte(line, '{')
	if open < 0 {
		return 0, "", false
	}
	n, err := strconv.Atoi(line[open+1 : len(line)-1])
	if err != nil || n < 0 {
		return 0, "", false
	}
	return n, line[:open], true
}

// Ne: LIST yanitlarindan \Sent isaretli klasorun adini bulur.
// Neden: Ad sunucuya gore degisiyor (burada INBOX.Sent, baskasinda Sent); isaret degismiyor.
func imap_sent_folder(lines []string) string {
	for _, line := range lines {
		rest, ok := strings.CutPrefix(line, "* LIST (")
		if !ok {
			continue
		}
		end := strings.IndexByte(rest, ')')
		if end < 0 {
			continue
		}
		is_sent := false
		for _, flag := range strings.Fields(rest[:end]) {
			if strings.EqualFold(flag, `\Sent`) {
				is_sent = true
			}
		}
		if !is_sent {
			continue
		}
		_, after_delim := imap_parse_astring(rest[end+1:])
		if name, _ := imap_parse_astring(after_delim); name != "" {
			return name
		}
	}
	return ""
}

// Tirnakli dizeyi (kacislari cozerek) ya da bosluga kadar atom'u okur.
func imap_parse_astring(s string) (val, rest string) {
	s = strings.TrimLeft(s, " ")
	if !strings.HasPrefix(s, `"`) {
		if i := strings.IndexByte(s, ' '); i >= 0 {
			return s[:i], s[i:]
		}
		return s, ""
	}
	var sb strings.Builder
	for i := 1; i < len(s); i++ {
		switch s[i] {
		case '\\':
			if i+1 < len(s) {
				i++
				sb.WriteByte(s[i])
			}
		case '"':
			return sb.String(), s[i+1:]
		default:
			sb.WriteByte(s[i])
		}
	}
	return sb.String(), ""
}

func imap_quote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// Yazdirilabilir ASCII tirnakla gider; digerleri (8-bit parola vb.) literal olarak.
func imap_astring(s string) any {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7e {
			return imap_literal(s)
		}
	}
	return imap_quote(s)
}
