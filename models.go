package main

import (
	"database/sql"
	"encoding/json"
)

type Account struct {
	id          int
	displayName string
	urlBase     string
	apiUrlBase  string
	accessToken string
}

type Channel struct {
	id          int
	displayName string
	system      sql.NullString
	queries     []string

	account *Account
}

func SelectChannels() ([]Channel, error) {
	res := make([]Channel, 0)
	accounts := make(map[int]Account)

	rows, err := Conn.Query(`
		select
			c.id, c.displayName, c.system, c.queries, a.id
		from
			channels as c,
			accounts as a
		where
			c.accountID = a.id;
	`)
	if err != nil {
		return nil, err
	}

	for rows.Next() {
		var accountID int
		var ch Channel
		var queries string

		if err := rows.Scan(&ch.id, &ch.displayName, &ch.system, &queries, &accountID); err != nil {
			return nil, err
		}
		err := json.Unmarshal([]byte(queries), &ch.queries)
		if err != nil {
			return nil, err
		}

		if a, ok := accounts[accountID]; ok {
			ch.account = &a
		} else {
			var a Account
			err := Conn.QueryRow(`
				select
					id, displayName, urlBase, apiUrlBase, accessToken
				from
					accounts
				where
					id = ?
			`, accountID).Scan(&a.id, &a.displayName, &a.urlBase, &a.apiUrlBase, &a.accessToken)
			if err != nil {
				return nil, err
			}
			ch.account = &a
		}
		res = append(res, ch)
	}

	return res, nil
}
