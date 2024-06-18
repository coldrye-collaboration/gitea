// Copyright 2024 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package v1_23 //nolint

import "xorm.io/xorm"

// TODO:silkentrance:migration
func AddTypeToIssueDependency(x *xorm.Engine) error {
	type IssueDependency struct {
		Type int `xorm:"UNIQUE(issue_dependency) NOT NULL DEFAULT 0"`
	}

	return x.Sync(new(IssueDependency))
}
