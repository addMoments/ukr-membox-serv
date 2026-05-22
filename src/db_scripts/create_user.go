package dbscripts

import (
	db "membox-serv/src/db_layer"
	"membox-serv/src/types"

	"github.com/huandu/go-sqlbuilder"
)

func CreateUser(
	name string,
	mail string,
	credentialType string,
	credentialValue string,
	isActive bool,
) (userUid string, err error) {
	res, err := db.Query_one(sqlbuilder.BuildNamed(`
		WITH new_user AS (
			INSERT INTO users (name, mail, is_active) 
			VALUES (${name}, ${mail}, ${isActive}) 
			RETURNING uid
		)
		INSERT INTO credentials (user_uid, type, value)
		SELECT uid, ${credentialType}, ${credentialValue} FROM new_user
		RETURNING user_uid
	`, types.Js_object{
		"name":            name,
		"mail":            mail,
		"credentialType":  credentialType,
		"credentialValue": credentialValue,
		"isActive":        isActive,
	}))
	if err != nil {
		return
	}

	userUid = string(res[0])
	return
}
