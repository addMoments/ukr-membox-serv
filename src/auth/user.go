package auth

import (
	"context"
	"errors"
	"fmt"
	db "membox-serv/src/db_layer"
	dbscripts "membox-serv/src/db_scripts"
	"membox-serv/src/env"
	networkutils "membox-serv/src/network_utils"
	"membox-serv/src/utils"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/huandu/go-sqlbuilder"
)

var ErrPackageLimitExceeded = errors.New("package limit exceeded")

const PackageLimitExceededCode = "PACKAGE_LIMIT_EXCEEDED"
const PackageLimitExceededMessage = "You have exceeded your package limit. Please contact help center."

func logGuestMetrics(eventUID string, source string) {
	guestLimit, err := dbscripts.Event_limit(eventUID, "guest_count")
	if err != nil {
		fmt.Printf("[guest-metrics] source=%s event_uid=%s err=limit_fetch_failed detail=%v\n", source, eventUID, err)
		return
	}

	guestCount, err := dbscripts.Event_guest_count(eventUID)
	if err != nil {
		fmt.Printf("[guest-metrics] source=%s event_uid=%s err=guest_count_failed detail=%v\n", source, eventUID, err)
		return
	}

	contributorCount, err := dbscripts.Event_contributor_count(eventUID)
	if err != nil {
		fmt.Printf("[guest-metrics] source=%s event_uid=%s err=contributor_count_failed detail=%v\n", source, eventUID, err)
		return
	}

	fmt.Printf(
		"[guest-metrics] source=%s event_uid=%s guest_count=%d contributor_count=%d guest_limit=%d\n",
		source,
		eventUID,
		guestCount,
		contributorCount,
		guestLimit,
	)
}

func EmailExists(email string) (exists bool, err error) {
	sb := sqlbuilder.NewSelectBuilder()
	sb.Select("COUNT(*)").From("users").Where(sb.Equal("mail", email))
	res, err := db.Query_one(sb)
	if err != nil {
		return
	}
	exists = string(res[0]) != "0"
	return
}

// AuthMiddleware is a unified middleware for both users and guests.
// IP validation is role-based (only enforced for "auth" role in ValidateToken).
func AuthMiddleware(next http.HandlerFunc, role string) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		var stat_code int
		var claims TokenClaims
		var redr_url string

		defer (func() {
			if redr_url != "" {
				http.Redirect(w, r, redr_url, http.StatusTemporaryRedirect)
				return
			}
			if err != nil {
				if errors.Is(err, ErrPackageLimitExceeded) {
					_ = networkutils.SendErrorJSON(
						w,
						http.StatusForbidden,
						PackageLimitExceededCode,
						PackageLimitExceededMessage,
					)
					return
				}
				if errors.Is(err, dbscripts.ErrEventClosed) {
					_ = networkutils.SendErrorJSON(
						w,
						http.StatusGone,
						"EVENT_CLOSED",
						networkutils.EventClosedMessage(r),
					)
					return
				}

				if stat_code == 0 {
					stat_code = 500
				}
				http.Error(w, err.Error(), stat_code)
				return
			}

			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), "claims", claims)))
		})()

		ip := GetClientIP(r)

		// Extract token using the centralized getter
		encToken, tokenErr := GetToken(r)
		if tokenErr != nil {
			if role == "webanon" {
				eventPackedUID := r.Header.Get("X-Event")
				eventUID := ""
				eventUID, err = utils.UUID.UnpackUUID(eventPackedUID)
				if err != nil {
					err = utils.Tag_err("gu3", err)
					return
				}
				logGuestMetrics(eventUID, "webanon.no-token")
				isClosed := false
				isClosed, err = dbscripts.Is_event_closed(eventUID)
				if err != nil {
					err = utils.Tag_err("gu3.1", err)
					return
				}
				if isClosed {
					err = dbscripts.ErrEventClosed
					return
				}

				fmt.Println("sd;fijseventUID")

				is_live := false
				is_live, err = dbscripts.Is_event_live(eventUID)
				if err != nil {
					fmt.Println("22sd;fijseventUdsfojID", err, is_live)
					err = utils.Tag_err("gu4", err)
					return
				}

				fmt.Println("sd;fijseventUdsfojID", is_live, err)
				if !is_live {
					err = errors.New("event is not live")
					return
				}
				fmt.Println("sd;fi111jseventUID")
				claims, _, err = Authorize(w, r, role, "", eventPackedUID)
				fmt.Println("sd;fi111jseventUID", claims, err)
				return
			}

			redr_url = "/signin"
			err = tokenErr
			stat_code = http.StatusUnauthorized
			return
		}

		claims, err = ValidateToken(encToken, ip)
		if err != nil {
			stat_code = 401
			return
		}

		if role == "webanon" && claims.Role == "webanon" {
			eventPackedUID := r.Header.Get("X-Event")
			eventUID := ""
			eventUID, err = utils.UUID.UnpackUUID(eventPackedUID)
			if err != nil {
				err = utils.Tag_err("gu3", err)
				return
			}
			logGuestMetrics(eventUID, "webanon.with-token")
			is_live := false
			is_live, err = dbscripts.Is_event_live(eventUID)
			if err != nil {
				err = utils.Tag_err("gu4", err)
				return
			}
			if !is_live {
				err = errors.New("event is not live")
				return
			}
			return
		}

		fmt.Println(claims.Role, role)

		if claims.Role != role {
			if role == "webanon" && claims.Role == "auth" {
				eventPackedUID := r.Header.Get("X-Event")
				eventUID := ""
				eventUID, err = utils.UUID.UnpackUUID(eventPackedUID)
				if err != nil {
					err = utils.Tag_err("gu3", err)
					return
				}
				isClosed := false
				isClosed, err = dbscripts.Is_event_closed(eventUID)
				if err != nil {
					err = utils.Tag_err("gu3.1", err)
					return
				}
				if isClosed {
					err = dbscripts.ErrEventClosed
					return
				}

				/* is_admin, err := dbscripts.Is_events_admin(eventUID, claims.UserUID)
				if err != nil {
					err = utils.Tag_err("gu4", err)
					return
				}
				if !is_admin {
					err = errors.New("unauthorized")
					return
				} */

				// Insert auth user into participants if not exists
				ib := sqlbuilder.NewInsertBuilder()
				ib.InsertInto("participants")
				ib.Cols("uid", "name", "event_uid")
				ib.Values(claims.UserUID, "admin", eventUID)
				ib.SQL("ON CONFLICT (uid) DO NOTHING")
				err = db.Exec(ib)
				if err != nil {
					err = utils.Tag_err("gu5", err)
					return
				}

				return
			}
			err = errors.New("unauthorized")
			stat_code = 401
			return
		}
	})
}

// Authorize creates a JWT token for the given role and user.
// - role: "auth" for authenticated users, "webanon" for anonymous guests
// - userUID: user identifier (if empty, a new UUID is generated for guests)
// Returns the JWT token that client should store and send via Authorization: Bearer header
func Authorize(w http.ResponseWriter, r *http.Request, role string, userUID string, eventPackedUID string) (claims TokenClaims, token string, err error) {
	// Generate userUID for guests if not provided
	if userUID == "" {
		if role == "auth" {
			err = errors.New("userUID is required for authenticated users")
			return
		}
		userUID = uuid.New().String()
	}

	// IP validation only applies to "auth" role
	ip := "-"
	if role == "auth" {
		ip = GetClientIP(r)
	}

	now := time.Now()
	claims = TokenClaims{
		Role:    role,
		UserUID: userUID,
		IP:      ip,
		Exp:     now.Add(tokenLife).Unix(),
		Iat:     now.Unix(),
	}

	fmt.Println("authorize", role, claims)

	token, err = claims.GenerateToken(env.Env().Jwt_secret)
	if err != nil {
		return
	}

	// Set token using the centralized setter
	SetToken(w, token)

	if role == "webanon" {
		var eventUID string
		var shortuuid string
		eventUID, err = utils.UUID.UnpackUUID(eventPackedUID)
		if err != nil {
			err = utils.Tag_err("au1", err)
			return
		}

		// Yeni guest event sayfasina girerken limiti contributor sayisina gore kontrol et.
		err = dbscripts.Check_contributor_limit_for_new_guest(eventUID)
		if err != nil {
			if errors.Is(err, dbscripts.ErrGuestLimitReached) || errors.Is(err, dbscripts.ErrLimitReached) {
				err = ErrPackageLimitExceeded
				return
			}
			err = utils.Tag_err("au1.0", err)
			return
		}

		newUUID := uuid.New().String()
		shortuuid, err = utils.UUID.PackUUID(newUUID)
		if err != nil {
			err = utils.Tag_err("au1.1", err)
			return
		}

		ib := sqlbuilder.NewInsertBuilder()
		ib.InsertInto("participants")
		ib.Cols("uid", "name", "event_uid")
		ib.Values(userUID, "guest-"+shortuuid, eventUID)
		err = db.Exec(ib)
		if err != nil {
			return
		}

	}

	r = r.WithContext(context.WithValue(r.Context(), "claims", claims))
	return
}
