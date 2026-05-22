package dbscripts

import (
	"errors"
	db "membox-serv/src/db_layer"
	"membox-serv/src/utils"

	"github.com/huandu/go-sqlbuilder"
)

var ErrEventClosed = errors.New("event is closed")

func Is_events_admin(eventUID string, userUID string) (is_admin bool, err error) {
	sb := sqlbuilder.NewSelectBuilder()
	sb.Select("COUNT(*)").From("events").Where(
		sb.Equal("uid", eventUID),
		sb.IsNotNull("deleted_at"),
	)

	res, err := db.Query_one(sb)
	if err != nil {
		err = utils.Tag_err("mce3", err)
		return
	}

	if string(res[0]) == "1" {
		err = ErrEventClosed
		return
	}

	sb = sqlbuilder.NewSelectBuilder()
	sb.Select("COUNT(*)").From("events").Where(
		sb.Equal("uid", eventUID),
		sb.IsNull("deleted_at"),
		sb.Var(userUID)+" = ANY(admins)",
	)

	res, err = db.Query_one(sb)
	if err != nil {
		err = utils.Tag_err("mce3", err)
		return
	}

	if string(res[0]) != "1" {
		err = errors.New("unauthorized")
		return
	}

	is_admin = true
	return

}
