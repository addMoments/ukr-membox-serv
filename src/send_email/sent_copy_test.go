package sendemail

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
)

// Canli Dovecot'un (mail.addmoments.com.ua) LIST yanitinin birebir bicimi, 2026-09-28.
var dovecot_list = []string{
	`* LIST (\HasChildren) "." INBOX`,
	`* LIST (\HasNoChildren \UnMarked \Archive) "." INBOX.Archive`,
	`* LIST (\HasNoChildren \UnMarked \Junk) "." INBOX.spam`,
	`* LIST (\HasNoChildren \UnMarked \Trash) "." INBOX.Trash`,
	`* LIST (\HasNoChildren \UnMarked \Sent) "." INBOX.Sent`,
	`* LIST (\HasNoChildren \UnMarked \Drafts) "." INBOX.Drafts`,
}

// Kodu yeniden kullanmadan, protokolu elle konusan kucuk bir IMAP sunucusu.
type fake_imap struct {
	list        []string
	append_resp string

	got_login  string
	got_folder string
	got_data   []byte
	verbs      []string
}

func (f *fake_imap) serve(conn net.Conn, done chan<- struct{}) {
	defer close(done)
	defer conn.Close()

	r := bufio.NewReader(conn)
	fmt.Fprint(conn, "* OK [CAPABILITY IMAP4rev1 LITERAL+ AUTH=PLAIN] Dovecot ready.\r\n")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		tag, rest, _ := strings.Cut(strings.TrimRight(line, "\r\n"), " ")
		verb, args, _ := strings.Cut(rest, " ")
		f.verbs = append(f.verbs, verb)

		switch verb {
		case "LOGIN":
			f.got_login = args
			fmt.Fprintf(conn, "%s OK [CAPABILITY IMAP4rev1] Logged in\r\n", tag)
		case "LIST":
			for _, l := range f.list {
				fmt.Fprint(conn, l+"\r\n")
			}
			fmt.Fprintf(conn, "%s OK List completed (0.001 + 0.000 secs).\r\n", tag)
		case "APPEND":
			// args: "<klasor>" (\Seen) {n}
			open := strings.LastIndexByte(args, '{')
			n, _ := strconv.Atoi(args[open+1 : len(args)-1])
			f.got_folder = strings.TrimSuffix(args[:open], ` (\Seen) `)
			fmt.Fprint(conn, "+ OK\r\n")
			f.got_data = make([]byte, n)
			io.ReadFull(r, f.got_data)
			r.ReadString('\n')
			fmt.Fprintf(conn, "%s %s\r\n", tag, f.append_resp)
		case "LOGOUT":
			fmt.Fprint(conn, "* BYE Logging out\r\n")
			fmt.Fprintf(conn, "%s OK Logout completed.\r\n", tag)
			return
		default:
			fmt.Fprintf(conn, "%s BAD Unknown command\r\n", tag)
		}
	}
}

func run_fake(t *testing.T, f *fake_imap, user, pass string, raw []byte) error {
	t.Helper()
	client, server := net.Pipe()
	done := make(chan struct{})
	go f.serve(server, done)
	err := imap_append_to_sent(client, user, pass, raw)
	client.Close()
	<-done
	return err
}

func TestSentCopyAppendsExactMessageToSentFolder(t *testing.T) {
	f := &fake_imap{list: dovecot_list, append_resp: "OK [APPENDUID 1770366831 1] Append completed."}
	raw := buildSample()

	if err := run_fake(t, f, "noreply@addmoments.com.ua", "secret", raw); err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}
	if f.got_login != `"noreply@addmoments.com.ua" "secret"` {
		t.Errorf("LOGIN argumanlari: %q", f.got_login)
	}
	if f.got_folder != `"INBOX.Sent"` {
		t.Errorf("APPEND klasoru: %q", f.got_folder)
	}
	if !bytes.Equal(f.got_data, raw) {
		t.Errorf("sunucuya giden mesaj gonderilenle ayni degil (%d / %d bayt)", len(f.got_data), len(raw))
	}
	if got := strings.Join(f.verbs, " "); got != "LOGIN LIST APPEND LOGOUT" {
		t.Errorf("komut sirasi: %s", got)
	}
}

func TestSentCopyReadsLiteralAndQuotedFolderNames(t *testing.T) {
	f := &fake_imap{
		list: []string{
			`* LIST (\HasNoChildren) "/" INBOX`,
			"* LIST (\\HasNoChildren \\Sent) \"/\" {10}\r\nSent \"Box\"",
		},
		append_resp: "OK Append completed.",
	}
	if err := run_fake(t, f, "u", "p", buildSample()); err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}
	if f.got_folder != `"Sent \"Box\""` {
		t.Errorf("literal klasor adi yanlis tirnaklandi: %q", f.got_folder)
	}

	quoted := imap_sent_folder([]string{`* LIST (\sent) "/" "Sent Items"`})
	if quoted != "Sent Items" {
		t.Errorf("tirnakli klasor adi: %q", quoted)
	}
}

func TestSentCopyQuotesPasswordSpecials(t *testing.T) {
	f := &fake_imap{list: dovecot_list, append_resp: "OK Append completed."}
	if err := run_fake(t, f, "noreply@addmoments.com.ua", `pa"ss\word`, buildSample()); err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}
	if f.got_login != `"noreply@addmoments.com.ua" "pa\"ss\\word"` {
		t.Errorf("parola kacislari: %q", f.got_login)
	}
}

func TestSentCopyWithoutSentFolderAppendsNothing(t *testing.T) {
	f := &fake_imap{list: dovecot_list[:4], append_resp: "OK"}
	err := run_fake(t, f, "u", "p", buildSample())
	if err == nil || !strings.Contains(err.Error(), `\Sent`) {
		t.Fatalf("\\Sent klasoru yokken hata bekleniyordu, gelen: %v", err)
	}
	if f.got_data != nil {
		t.Error("klasor bulunmadan APPEND gonderildi")
	}
	if got := strings.Join(f.verbs, " "); got != "LOGIN LIST LOGOUT" {
		t.Errorf("komut sirasi: %s", got)
	}
}

func TestSentCopyReportsRejectedAppend(t *testing.T) {
	f := &fake_imap{list: dovecot_list, append_resp: "NO [OVERQUOTA] Quota exceeded (mailbox for user is full)"}
	err := run_fake(t, f, "u", "p", buildSample())
	if err == nil || !strings.Contains(err.Error(), "OVERQUOTA") {
		t.Fatalf("reddedilen APPEND hata olarak donmeli, gelen: %v", err)
	}
}
