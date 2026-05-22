package routes

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"membox-serv/src/auth"
	db "membox-serv/src/db_layer"
	dbscripts "membox-serv/src/db_scripts"
	"membox-serv/src/env"
	networkutils "membox-serv/src/network_utils"
	"membox-serv/src/payments"
	sendemail "membox-serv/src/send_email"
	"membox-serv/src/types"
	"membox-serv/src/utils"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/huandu/go-sqlbuilder"
)

type collaborator_routes_typ struct{}

var CollaboratorRoutes collaborator_routes_typ

type new_collaborator_req struct {
	Email string `json:"email"`
}

func (cr collaborator_routes_typ) Delete(w http.ResponseWriter, r *http.Request) {
	var stat_code = 0
	var payload interface{}
	var err error

	defer (func() {
		if err != nil {
			if errors.Is(err, dbscripts.ErrEventClosed) {
				_ = networkutils.SendErrorJSON(w, http.StatusGone, "EVENT_CLOSED", networkutils.EventClosedMessage(r))
				return
			}
			if stat_code == 0 {
				stat_code = 500
			}
			http.Error(w, err.Error(), stat_code)
			return
		}

		networkutils.SendJson(payload, w)
	})()

	claims, ok := r.Context().Value("claims").(auth.TokenClaims)
	if !ok {
		err = errors.New("unauthorized")
		return
	}

	packedCollaboratorUid := mux.Vars(r)["packedCollaboratorUid"]
	fmt.Printf("[collab.Delete] request_user_uid=%s packed_collaborator_uid=%s\n", claims.UserUID, packedCollaboratorUid)

	collaboratorUid, err := utils.UUID.UnpackUUID(packedCollaboratorUid)
	if err != nil {
		fmt.Printf("[collab.Delete] fail=unpack_collaborator_uid request_user_uid=%s packed_collaborator_uid=%s err=%v\n", claims.UserUID, packedCollaboratorUid, err)
		err = errors.New("failed to unpack collaborator uid")
		return
	}
	fmt.Printf("[collab.Delete] request_user_uid=%s target_collaborator_uid=%s check=resolve_event_by_shared_admins\n", claims.UserUID, collaboratorUid)

	sb := sqlbuilder.NewSelectBuilder()
	sb.Select("uid", "purchase_uid").From("events").Where(
		fmt.Sprintf("'%s' = ANY(admins)", claims.UserUID),
		fmt.Sprintf("'%s' = ANY(admins)", collaboratorUid),
		sb.IsNull("deleted_at"),
	)

	res, err := db.Query_one(sb)
	if err != nil {
		fmt.Printf("[collab.Delete] fail=query_events request_user_uid=%s target_collaborator_uid=%s err=%v\n", claims.UserUID, collaboratorUid, err)
		err = errors.New("failed to query events")
		return
	}

	eventUid := string(res[0])
	if eventUid == "" {
		fmt.Printf("[collab.Delete] fail=collaborator_not_found request_user_uid=%s target_collaborator_uid=%s\n", claims.UserUID, collaboratorUid)
		err = errors.New("collaborator not found")
		return
	}

	purchaseUid := string(res[1])
	if purchaseUid == "" {
		fmt.Printf("[collab.Delete] fail=missing_purchase_uid request_user_uid=%s target_collaborator_uid=%s resolved_event_uid=%s\n", claims.UserUID, collaboratorUid, eventUid)
		err = errors.New("purchase uid not found")
		return
	}
	fmt.Printf("[collab.Delete] request_user_uid=%s target_collaborator_uid=%s resolved_event_uid=%s resolved_purchase_uid=%s check=buyer_match\n", claims.UserUID, collaboratorUid, eventUid, purchaseUid)

	sb = sqlbuilder.NewSelectBuilder()
	sb.Select("COUNT(*)").From("purchases").Where(
		sb.Equal("uid", purchaseUid),
		sb.Equal("buyer_uid", claims.UserUID),
	)

	res, err = db.Query_one(sb)
	if err != nil {
		fmt.Printf("[collab.Delete] fail=query_purchase_buyer request_user_uid=%s target_collaborator_uid=%s resolved_event_uid=%s resolved_purchase_uid=%s err=%v\n", claims.UserUID, collaboratorUid, eventUid, purchaseUid, err)
		err = errors.New("failed to query purchases")
		return
	}

	if string(res[0]) != "1" {
		fmt.Printf("[collab.Delete] fail=buyer_check request_user_uid=%s target_collaborator_uid=%s resolved_event_uid=%s resolved_purchase_uid=%s buyer_match_count=%s\n", claims.UserUID, collaboratorUid, eventUid, purchaseUid, string(res[0]))
		err = errors.New("user is not the buyer of the purchase")
		return
	}

	ub := sqlbuilder.NewUpdateBuilder()
	ub.Update("events").Set(
		fmt.Sprintf("admins = array_remove(admins, '%s'::uuid)", collaboratorUid),
	).Where(
		ub.Equal("uid", eventUid),
		ub.IsNull("deleted_at"),
	)
	err = db.Exec(ub)
	if err != nil {
		fmt.Printf("[collab.Delete] fail=update_event_admins request_user_uid=%s target_collaborator_uid=%s resolved_event_uid=%s resolved_purchase_uid=%s err=%v\n", claims.UserUID, collaboratorUid, eventUid, purchaseUid, err)
		err = errors.New("failed to update event admins")
		return
	}
	fmt.Printf("[collab.Delete] ok request_user_uid=%s target_collaborator_uid=%s resolved_event_uid=%s resolved_purchase_uid=%s\n", claims.UserUID, collaboratorUid, eventUid, purchaseUid)

	payload = "ok"

}

func (cr collaborator_routes_typ) DeleteByEvent(w http.ResponseWriter, r *http.Request) {
	var stat_code = 0
	var payload interface{}
	var err error

	defer (func() {
		if err != nil {
			if errors.Is(err, dbscripts.ErrEventClosed) {
				_ = networkutils.SendErrorJSON(w, http.StatusGone, "EVENT_CLOSED", networkutils.EventClosedMessage(r))
				return
			}
			if stat_code == 0 {
				stat_code = 500
			}
			http.Error(w, err.Error(), stat_code)
			return
		}

		networkutils.SendJson(payload, w)
	})()

	claims, ok := r.Context().Value("claims").(auth.TokenClaims)
	if !ok {
		err = errors.New("unauthorized")
		return
	}

	packedEventUid := mux.Vars(r)["packedEventUid"]
	packedCollaboratorUid := mux.Vars(r)["packedCollaboratorUid"]
	fmt.Printf("[collab.DeleteByEvent] request_user_uid=%s packed_event_uid=%s packed_collaborator_uid=%s\n", claims.UserUID, packedEventUid, packedCollaboratorUid)

	eventUid, err := utils.UUID.UnpackUUID(packedEventUid)
	if err != nil {
		fmt.Printf("[collab.DeleteByEvent] fail=unpack_event_uid request_user_uid=%s packed_event_uid=%s err=%v\n", claims.UserUID, packedEventUid, err)
		err = errors.New("failed to unpack event uid")
		return
	}

	collaboratorUid, err := utils.UUID.UnpackUUID(packedCollaboratorUid)
	if err != nil {
		fmt.Printf("[collab.DeleteByEvent] fail=unpack_collaborator_uid request_user_uid=%s packed_collaborator_uid=%s err=%v\n", claims.UserUID, packedCollaboratorUid, err)
		err = errors.New("failed to unpack collaborator uid")
		return
	}

	isAdmin, err := dbscripts.Is_events_admin(eventUid, claims.UserUID)
	if errors.Is(err, dbscripts.ErrEventClosed) {
		return
	}
	if err != nil || !isAdmin {
		fmt.Printf("[collab.DeleteByEvent] fail=admin_check request_user_uid=%s resolved_event_uid=%s err=%v\n", claims.UserUID, eventUid, err)
		err = errors.New("unauthorized")
		return
	}

	sb := sqlbuilder.NewSelectBuilder()
	sb.Select("COUNT(*)").From("events").Where(
		sb.Equal("uid", eventUid),
		fmt.Sprintf("'%s' = ANY(admins)", collaboratorUid),
		sb.IsNull("deleted_at"),
	)

	res, err := db.Query_one(sb)
	if err != nil {
		fmt.Printf("[collab.DeleteByEvent] fail=query_event_collaborator request_user_uid=%s target_collaborator_uid=%s resolved_event_uid=%s err=%v\n", claims.UserUID, collaboratorUid, eventUid, err)
		err = errors.New("failed to query events")
		return
	}

	if string(res[0]) != "1" {
		fmt.Printf("[collab.DeleteByEvent] fail=collaborator_not_in_event request_user_uid=%s target_collaborator_uid=%s resolved_event_uid=%s\n", claims.UserUID, collaboratorUid, eventUid)
		err = errors.New("collaborator not found")
		return
	}

	ub := sqlbuilder.NewUpdateBuilder()
	ub.Update("events").Set(
		fmt.Sprintf("admins = array_remove(admins, '%s'::uuid)", collaboratorUid),
	).Where(
		ub.Equal("uid", eventUid),
		ub.IsNull("deleted_at"),
	)
	err = db.Exec(ub)
	if err != nil {
		fmt.Printf("[collab.DeleteByEvent] fail=update_event_admins request_user_uid=%s target_collaborator_uid=%s resolved_event_uid=%s err=%v\n", claims.UserUID, collaboratorUid, eventUid, err)
		err = errors.New("failed to update event admins")
		return
	}

	fmt.Printf("[collab.DeleteByEvent] ok request_user_uid=%s target_collaborator_uid=%s resolved_event_uid=%s\n", claims.UserUID, collaboratorUid, eventUid)
	payload = "ok"
}

func (cr collaborator_routes_typ) New(w http.ResponseWriter, r *http.Request) {
	var stat_code = 0
	var payload interface{}
	var err error

	defer (func() {
		if err != nil {
			if errors.Is(err, dbscripts.ErrEventClosed) {
				_ = networkutils.SendErrorJSON(w, http.StatusGone, "EVENT_CLOSED", networkutils.EventClosedMessage(r))
				return
			}
			if stat_code == 0 {
				stat_code = 500
			}
			http.Error(w, err.Error(), stat_code)
			return
		}

		networkutils.SendJson(payload, w)
	})()

	claims, ok := r.Context().Value("claims").(auth.TokenClaims)
	if !ok {
		err = errors.New("unauthorized")
		return
	}

	packedEventUid := mux.Vars(r)["packedEventUid"]
	eventUid, err := utils.UUID.UnpackUUID(packedEventUid)
	if err != nil {
		err = errors.New("failed to unpack event uid")
		return
	}

	is_admin, err := dbscripts.Is_events_admin(eventUid, claims.UserUID)
	if errors.Is(err, dbscripts.ErrEventClosed) {
		return
	}
	if err != nil {
		err = utils.Tag_err("gu2", err)
		return
	}
	if !is_admin {
		err = errors.New("unauthorized")
		return
	}

	byt, err := io.ReadAll(r.Body)
	if err != nil {
		err = errors.New("failed to read body")
		return
	}

	var req new_collaborator_req
	err = json.Unmarshal(byt, &req)
	if err != nil {
		err = errors.New("failed to unmarshal body")
		return
	}
	/*
		ub := sqlbuilder.NewUpdateBuilder()
		ub.Update("events").Set(
			ub.Assign("admins", sqlbuilder.Raw(fmt.Sprintf("array_append(admins, '%s'::uuid)", userUid))),
		).Where(ub.Equal("uid", eventUid))
		err = db.Exec(ub)
		if err != nil {
			err = errors.New("failed to update event admins")
			return
		}*/

	fmt.Printf("[collab.New] building invite token for email=%s eventUid=%s\n", req.Email, eventUid)

	pt := payments.PaymentToken{
		Provider:    "admt_payment",
		ReferenceNo: packedEventUid,
		Status:      "c:" + req.Email,
	}

	encPaymentTkn, err := pt.Encrypt(env.Env().PaymentSecret)
	if err != nil {
		fmt.Printf("[collab.New] ERROR: failed to encrypt payment token: %v\n", err)
		err = errors.New("failed to encrypt payment token")
		return
	}

	fmt.Printf("[collab.New] token encrypted OK, checking SMTP connection\n")

	if connErr := sendemail.Info_mail.CheckConn(); connErr != nil {
		fmt.Printf("[collab.New] SMTP connection unhealthy (%v), will attempt reconnect on send\n", connErr)
	} else {
		fmt.Printf("[collab.New] SMTP connection OK\n")
	}

	signupLink := "https://addmoments.com.ua/signup/" + encPaymentTkn
	fmt.Printf("[collab.New] sending invite email to=%s link=%s\n", req.Email, signupLink)

	mailErr := sendemail.Info_mail.Send([]string{req.Email}, "Add Moments Collaborator Invitation", func(w io.WriteCloser) {
		sendemail.Write_html(w, "You've been invited!", []string{
			"You have been invited to collaborate on an event on Add Moments. Click the button below to create your account.",
			sendemail.Button(signupLink, "Accept Invitation"),
			"If the button doesn't work, copy and paste this link into your browser:<br>" + signupLink,
		})
	}, nil)

	if mailErr != nil {
		fmt.Printf("[collab.New] ERROR: failed to send invite email to=%s err=%v\n", req.Email, mailErr)
	} else {
		fmt.Printf("[collab.New] invite email sent successfully to=%s\n", req.Email)
	}

	payload = types.Js_object{
		"token": encPaymentTkn,
	}

}
