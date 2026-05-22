package networkutils

import (
	"membox-serv/src/types"
	"net/http"
)

func ToMessageScreen(w http.ResponseWriter, r *http.Request, m types.MessageScreen) {
	var is_temp bool
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
	})()

	m_encoded, err := m.Encode()
	if err != nil {
		return
	}

	redr_url = "/notice/" + m_encoded
}
