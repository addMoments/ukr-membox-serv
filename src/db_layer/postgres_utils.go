package db

import (
	"encoding/json"
	"fmt"
)

func Conflict_update(cols []string, conflict_column string) (qry string) {
	qry += fmt.Sprintf("ON CONFLICT (%s) DO UPDATE SET ", conflict_column)

	for i := 0; i < len(cols); i++ {
		qry += fmt.Sprintf("%s = EXCLUDED.%s", cols[i], cols[i])
		if i+1 == len(cols) {
			qry += " "
		} else {
			qry += ", "
		}
	}

	return

}

func Pg_ar[Ar ~[]E, E any](vals Ar) (r interface{}) {
	s := fmt.Sprintf("{%s}", Pg_stringify(vals))
	fmt.Println(s, "pg ar")
	return s
}

func Pg_stringify[Ar ~[]E, E any](vals Ar) (r string) {
	r = ""
	for i := 0; i < len(vals); i++ {
		r = fmt.Sprintf("%s%v", r, vals[i])
		if i+1 < len(vals) {
			r += ", "
		}
	}
	r += ""
	return
}

func Parse_pg_ar[P ~*[]E, E any](ar_byt []byte, dest_ptr P) (err error) {
	if len(ar_byt) == 0 {
		err = fmt.Errorf("parse pg ar: no bytes")
		return
	}
	err = json.Unmarshal([]byte(fmt.Sprintf("[%s]", string(ar_byt[1:len(ar_byt)-1]))), dest_ptr)

	return

}

func Interface_ar[E any](vals []E) (r []interface{}) {
	r = make([]interface{}, len(vals))
	for i := 0; i < len(vals); i++ {
		r[i] = vals[i]
	}
	return
}
