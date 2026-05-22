package dbscripts

import (
	db "membox-serv/src/db_layer"

	"github.com/huandu/go-sqlbuilder"
)

func Is_event_closed(eventUID string) (closed bool, err error) {
	sb := sqlbuilder.NewSelectBuilder()
	sb.Select("COUNT(*)").From("events").Where(
		sb.Equal("uid", eventUID),
		sb.IsNotNull("deleted_at"),
	)

	res, err := db.Query_one(sb)
	if err != nil {
		return
	}

	closed = string(res[0]) == "1"
	return
}
