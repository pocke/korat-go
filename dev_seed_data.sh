#!/bin/bash

set -e

sqlite3 ~/.cache/korat/development.sqlite3 << END
  PRAGMA foreign_keys = ON;

  replace into accounts(id, displayName, urlBase, apiUrlBase, accessToken, _createdAt, _updatedAt) VALUES (
    1,
    'GitHub.com',
    'https://github.com',
    'https://api.github.com',
    '${GITHUB_ACCESS_TOKEN}',
    $(date '+%s'),
    $(date '+%s')
  );

  replace into channels (id, displayName, queries, accountID, _createdAt, _updatedAt) VALUES (
    1,
    'me',
    '["involves:pocke","user:pocke"]',
    1,
    $(date '+%s'),
    $(date '+%s')
  );

  replace into channels (id, displayName, queries, accountID, _createdAt, _updatedAt) VALUES (
    2,
    'RuboCop',
    '["user:rubocop-hq"]',
    1,
    $(date '+%s'),
    $(date '+%s')
  );
END
