package main

import (
	"database/sql"
	"strings"

	"github.com/jinzhu/gorm"
	"github.com/pkg/errors"
)

type Qc struct {
	q string
	c Channel
}

type QDB struct {
	ID        int `gorm:"primary_key"`
	Str       string
	Type      string
	Owner     string
	Name      sql.NullString
	ChannelID int
	AccountID int
}

type QueryOptimizer struct {
	queries       []Qc
	actualQueries []ActualQuery
}

func (o *QueryOptimizer) assign(c Channel, q string) {
	if isOptimizableQuery(q) {
		o.queries = append(o.queries, Qc{q, c})
	} else {
		aq := ActualQuery{
			query:       q,
			conditions:  []Condition{{channel: c}},
			accessToken: c.Account.AccessToken,
		}
		o.actualQueries = append(o.actualQueries, aq)
	}
}

// TODO support multi account
func (o *QueryOptimizer) Optimize() ([]ActualQuery, error) {
	db, err := NewTmpDatabaseForOptimize()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	for _, qc := range o.queries {
		for _, q := range strings.Split(qc.q, " ") {
			s := strings.Split(q, ":")
			qType := s[0]
			body := s[1]

			qdb := QDB{
				Str:       q,
				Type:      qType,
				ChannelID: qc.c.ID,
				AccountID: qc.c.Account.ID,
			}

			switch qType {
			case "repo":
				x := strings.Split(body, "/")
				owner := x[0]
				name := x[1]
				qdb.Owner = owner
				qdb.Name = sql.NullString{Valid: true, String: name}
				err := db.Create(&qdb).Error
				if err != nil {
					return nil, err
				}
			case "user":
				qdb.Owner = body
				err := db.Create(&qdb).Error
				if err != nil {
					return nil, err
				}
			default:
				return nil, errors.New("Unreachable")
			}
		}
	}

	owners, err := frequentOwners(db)
	if err != nil {
		return nil, err
	}

	qdbs := make([]QDB, 0)
	err = db.Where("owner NOT IN (?)", owners).Find(&qdbs).Error
	if err != nil {
		return nil, err
	}

	return o.actualQueries, nil
}

func frequentOwners(db *gorm.DB) ([]string, error) {
	rows, err := db.Raw(`
		select owner
		from q
		where
			type = 'repo' AND
			owner not in (
				select owner
				from q
				where type = 'user'
			)
		group by
			owner
		order by
			count(id) desc
		limit 5;
	`).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	res := make([]string, 0, 5)
	for rows.Next() {
		var str string
		err := rows.Scan(str)
		if err != nil {
			return nil, err
		}
		res = append(res, str)
	}
	return res, nil
}

func NewTmpDatabaseForOptimize() (*gorm.DB, error) {
	db, err := gorm.Open("sqlite3", ":memory:")
	if err != nil {
		return nil, err
	}
	err = db.Exec(`
		create table q (
			id integer not null primary key,
			str string not null primary key,
			type string not null,
			owner string not null,
			name string,
			channel_id integer not null,
			account_id integer not null
		);
	`).Error
	if err != nil {
		db.Close()
		return nil, err
	}

	return db, nil
}

func isOptimizableQuery(q string) bool {
	qs := strings.Split(q, " ")
	for _, s := range qs {
		if !strings.HasPrefix(s, "repo:") && !strings.HasPrefix(s, "user:") {
			return false
		}
	}
	return true
}
