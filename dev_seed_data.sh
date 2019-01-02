#!/bin/bash

set -e

sqlite3 ~/.cache/korat/development.sqlite3 << END
  PRAGMA foreign_keys = ON;

  replace into accounts(id, displayName, urlBase, apiUrlBase, accessToken) VALUES (
    1,
    'GitHub.com',
    'https://github.com',
    'https://api.github.com',
    '${GITHUB_ACCESS_TOKEN}'
  );

  replace into channels (id, displayName, queries, accountID) VALUES (
    1,
    'me',
    '["involves:pocke","user:pocke"]',
    1
  );

  replace into channels (id, displayName, queries, accountID) VALUES (
    2,
    'RuboCop',
    '["user:rubocop-hq"]',
    1
  );

  replace into channels (id, displayName, queries, accountID, system) VALUES (
    3,
    'Teams',
    '[]',
    1,
    'teams'
  );

  replace into channels (id, displayName, queries, accountID, system) VALUES (
    4,
    'Watching',
    '[]',
    1,
    'watching'
  );
END
