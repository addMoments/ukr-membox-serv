package sendemail

import (
	"fmt"
	"io"
	"strings"
	"text/template"
)

func A(href, content string) string {
	return fmt.Sprintf(`<a href="%s" style="text-decoration: underline;">%s</a>`, href, content)
}

func Button(href, label string) string {
	return fmt.Sprintf(`<a href="%s" style="display:inline-flex;align-items:center;justify-content:center;gap:12px;background:#D2E823;color:#254f1a;padding:20px 40px;border-radius:9999px;font-size:20px;font-weight:700;font-family:Poppins,sans-serif;text-decoration:none;border:none;cursor:pointer;box-shadow:0 10px 30px rgba(210,232,35,0.3);margin:16px 0;">%s &#8594;</a>`, href, label)
}

func Write_html(w io.Writer, header string, content []string) {
	mail_template(header, content, w)
}

func mail_template(
	header string,
	content []string,
	msg_writer io.Writer,

) error {
	tmpl := template.Must(template.New("mail_template").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta http-equiv="X-UA-Compatible" content="IE=edge">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">

</head>
<body style="margin: 0; padding: 0; background-color: #f7f7f7;">
    <center>
        <table width="100%" border="0" cellpadding="0" cellspacing="0" bgcolor="#f6f6f6">
            <tr>
                <td align="center" valign="top">
                    <table width="600" border="0" cellpadding="0" cellspacing="0" class="inner-table" style="background-color: #ffffff; margin-top: 15px; box-shadow: 0px 0px 5px 0px rgba(0,0,0,0.2);">
                        <tr>
                            <td style="padding: 20px;">
								<!-- Head -->
                                <table width="100%" border="0" cellpadding="0" cellspacing="0" style="padding-top: 20px;">
                                    <tr>
                                        <td class="content-padding" style="text-align: left; font-size: 24px; line-height: 20px; font-family: Poppins, sans-serif; color: #212529; padding: 0 20px; font-weight: 500;">
                                            {{.Header}}
                                        </td>
                                    </tr>
                                </table>

                                <!-- Body -->
                                <table width="100%" border="0" cellpadding="0" cellspacing="0" style="padding-top: 20px;">
                                    <tr>
                                        <td class="content-padding" style="text-align: left; font-size: 16px; line-height: 20px; font-family: Poppins, sans-serif; color: #2d3339; padding: 0 20px; font-weight: 400;">
                                            {{.Body}}
                                        </td>
                                    </tr>
                                </table>
                                <!-- Footer -->
                                <table width="100%" border="0" cellpadding="0" cellspacing="0" style="padding-top: 20px;">
                                    <tr>
                                        <td style="text-align: center; font-size: 12px; line-height: 18px; font-family: Poppins, sans-serif; color: #666666; padding: 20px;">
                                            © 2024 Nanbis Ltd., Tum haklari saklidir.
                                            <br>
                                            <a href="https://kakooo.co/privacy/tr" style="color: #666666; text-decoration: underline;">Gizlilik sozlesmesi</a>
                                        </td>
                                    </tr>
                                </table>
                            </td>
                        </tr>
                    </table>
                </td>
            </tr>
        </table>
    </center>
</body>
</html>`))

	return tmpl.Execute(msg_writer, map[string]string{
		"Header": header,
		"Body":   strings.Join(content, "<br><br>"),
	})
}
