package main

import (
	"context"
	"database/sql"

	"github.com/jinzhu/gorm"
	"github.com/pkg/errors"
)

func dbMigrate() error {
	row := gormConn.Raw(`select name from sqlite_master where type='table' and name='migration_info'`).Row()
	var blackhole string
	err := row.Scan(&blackhole)
	if err == sql.ErrNoRows {
		err := gormConn.Exec(`create table migration_info (
			id integer not null primary key
		)`).Error
		if err != nil {
			return errors.WithStack(err)
		}
	} else if err != nil {
		return errors.WithStack(err)
	}

	err = doMigration(1, `
		create table accounts (
			id          integer not null primary key,
			displayName string not null,
			urlBase     string not null,
			apiUrlBase  string not null,
			accessToken string not null
		);

		create table channels (
			id            integer not null primary key,
			displayName   string not null,
			system        string,
			queries       string not null,

			accountID     integer not null,

			FOREIGN KEY(accountID) REFERENCES accounts(id) ON UPDATE CASCADE ON DELETE CASCADE
		);

		create table github_users (
			id          integer not null primary key,
			login       string not null,
			avatarURL   string not null
		);

		create table issues (
			id            integer not null primary key,
			number        integer not null,
			title         string not null,
			userID        integer not null,
			repoOwner     string not null,
			repoName      string not null,
			state         string not null,
			locked        bool not null,
			comments      integer not null,
			createdAt     string not null,
			updatedAt     string not null,
			closedAt      string,
			isPullRequest boolean not null,
			body          string not null,
			alreadyRead   boolean not null,
			milestoneID   integer,

			FOREIGN KEY(userID) REFERENCES github_users(id) ON UPDATE CASCADE ON DELETE CASCADE
			FOREIGN KEY(milestoneID) REFERENCES milestones(id) ON UPDATE CASCADE ON DELETE CASCADE
		);

		create table labels (
			id          integer not null primary key,
			name        string not null,
			color       string not null,
			'default'   boolean not null
		);

		create table milestones (
			id            integer not null primary key,
			number        integer not null,
			title         string not null,
			description   string not null,
			state         string not null,
			createdAt     string not null,
			updatedAt     string not null,
			closedAt      string
		);

		create table assigned_labels_to_issue (
			id          integer not null primary key,
			issueID     integer not null,
			labelID     integer not null,

			FOREIGN KEY(issueID) REFERENCES issues(id) ON UPDATE CASCADE ON DELETE CASCADE
			FOREIGN KEY(labelID) REFERENCES labels(id) ON UPDATE CASCADE ON DELETE CASCADE
		);
		create unique index uniq_issue_label on assigned_labels_to_issue(issueID, labelID);

		create table assigned_users_to_issue (
			id          integer not null primary key,
			issueID     integer not null,
			userID      integer not null,

			FOREIGN KEY(issueID) REFERENCES issues(id) ON UPDATE CASCADE ON DELETE CASCADE
			FOREIGN KEY(userID) REFERENCES github_users(id) ON UPDATE CASCADE ON DELETE CASCADE
		);
		create unique index uniq_assigned_user_to_issue on assigned_users_to_issue(issueID, userID);
	`)
	if err != nil {
		return errors.WithStack(err)
	}

	err = doMigration(2, `
		create table queries (
			id            integer not null primary key,
			query         string not null
		);

		create table channel_issues (
			id            integer not null primary key,
			issueID       integer not null,
			channelID     integer not null,
			queryID       integer not null,

			FOREIGN KEY(issueID) REFERENCES issues(id) ON UPDATE CASCADE ON DELETE CASCADE
			FOREIGN KEY(channelID) REFERENCES channels(id) ON UPDATE CASCADE ON DELETE CASCADE
			FOREIGN KEY(queryID) REFERENCES queries(id) ON UPDATE CASCADE ON DELETE CASCADE
		);
		create unique index uniq_channel_issue on channel_issues(issueID, channelID, queryID);
	`)
	if err != nil {
		return errors.WithStack(err)
	}

	err = doMigration(3, `
		create index fk_channel_account_id on channels(accountID);
		create index fk_issue_user_id on issues(userID);
		create index fk_issue_milestone_id on issues(milestoneID);
	`)
	if err != nil {
		return errors.WithStack(err)
	}

	// Change order of index
	err = doMigration(4, `
		drop index uniq_channel_issue;
		create unique index uniq_channel_issue on channel_issues(channelID, issueID, queryID);
	`)
	if err != nil {
		return errors.WithStack(err)
	}

	err = doMigration(5, `
		alter table issues add column merged boolean;
	`)
	if err != nil {
		return errors.WithStack(err)
	}

	return nil
}

func doMigration(id int, query string) error {
	return txGorm(func(tx *gorm.DB) error {
		res := gormConn.First(&MigrationInfo{ID: id})
		exist := res.RecordNotFound()
		if exist && res.Error != nil {
			return res.Error
		}

		if exist {
			return nil
		}

		err := tx.Exec(query).Error
		if err != nil {
			return errors.WithStack(err)
		}
		err = tx.Exec(`insert into migration_info(id) values(?)`, id).Error
		if err != nil {
			return errors.WithStack(err)
		}
		return nil
	})
}

type sqlConn interface {
	QueryRowContext(context.Context, string, ...interface{}) *sql.Row
	ExecContext(context.Context, string, ...interface{}) (sql.Result, error)
}
