// Copyright (c) 2025 Tulir Asokan
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package upgrades

import (
	"embed"
)

// FS exposes the migration files to the pgx-based upgrade runner in
// store/sqlstore/upgrade.go. Upstream's dbutil upgrade table is not used here
// because the multitenant store runs on pgxpool instead of database/sql.
//
//go:embed *.sql
var FS embed.FS
