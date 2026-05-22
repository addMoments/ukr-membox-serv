package dbscripts

// import (
// 	db "membox-serv/src/db_layer"
// 	"time"

// 	"github.com/huandu/go-sqlbuilder"
// )

func Is_event_live(eventUID string) (live bool, err error) {
	// sb := sqlbuilder.NewSelectBuilder()
	// sb.Select("COUNT(*)").From("events").Where(sb.Equal("uid", eventUID), sb.LE("activation_date", time.Now()), sb.GE("active_until", time.Now()))
	// var res [][]byte
	// res, err = db.Query_one(sb)
	// if err != nil {
	// 	return
	// }
	// live = string(res[0]) == "1"
	return true, nil
}
