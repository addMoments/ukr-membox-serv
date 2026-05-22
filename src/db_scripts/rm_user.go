package dbscripts

import (
	db "membox-serv/src/db_layer"
	"membox-serv/src/types"

	"github.com/huandu/go-sqlbuilder"
)

func RemoveUser(userUID string) error {
	_, err := db.Query_one(sqlbuilder.BuildNamed(`
		WITH deactivate_user AS (
			UPDATE users SET 
				mail = gen_random_uuid()::text,
				name = 'Deleted User',
				surname = '',
				is_active = false
			WHERE uid = ${userUID}
			RETURNING uid
		),
		clear_credentials AS (
			UPDATE credentials SET value = ''
			WHERE user_uid = ${userUID}
		),
		suspend_sole_admin_events AS (
			UPDATE events SET status = 'suspended'
			WHERE admins = ARRAY[${userUID}]::uuid[]
		),
		remove_from_multi_admin_events AS (
			UPDATE events SET admins = array_remove(admins, ${userUID}::uuid)
			WHERE ${userUID}::uuid = ANY(admins) AND array_length(admins, 1) > 1
		)
		SELECT uid FROM deactivate_user
	`, types.Js_object{
		"userUID": userUID,
	}))

	return err
}
