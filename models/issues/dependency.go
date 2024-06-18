// Copyright 2018 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package issues

import (
	"context"
	"fmt"

	"code.gitea.io/gitea/models/db"
	user_model "code.gitea.io/gitea/models/user"
	"code.gitea.io/gitea/modules/timeutil"
	"code.gitea.io/gitea/modules/util"
)

// ErrDependencyExists represents a "DependencyAlreadyExists" kind of error.
type ErrDependencyExists struct {
	IssueID        int64
	DependencyID   int64
	DependencyType string
}

// IsErrDependencyExists checks if an error is a ErrDependencyExists.
func IsErrDependencyExists(err error) bool {
	_, ok := err.(ErrDependencyExists)
	return ok
}

func (err ErrDependencyExists) Error() string {
	return fmt.Sprintf("issue dependency does already exist [issue id: %d, dependency id: %d]", err.IssueID, err.DependencyID)
}

func (err ErrDependencyExists) Unwrap() error {
	return util.ErrAlreadyExist
}

// ErrDependencyNotExists represents a "DependencyAlreadyExists" kind of error.
type ErrDependencyNotExists struct {
	IssueID        int64
	DependencyID   int64
	DependencyType string
}

// IsErrDependencyNotExists checks if an error is a ErrDependencyExists.
func IsErrDependencyNotExists(err error) bool {
	_, ok := err.(ErrDependencyNotExists)
	return ok
}

func (err ErrDependencyNotExists) Error() string {
	return fmt.Sprintf("issue dependency does not exist [issue id: %d, dependency id: %d, type: %s]", err.IssueID, err.DependencyID, err.DependencyType)
}

func (err ErrDependencyNotExists) Unwrap() error {
	return util.ErrNotExist
}

// ErrCircularDependency represents a "DependencyCircular" kind of error.
type ErrCircularDependency struct {
	IssueID        int64
	DependencyID   int64
	DependencyType string
}

// IsErrCircularDependency checks if an error is a ErrCircularDependency.
func IsErrCircularDependency(err error) bool {
	_, ok := err.(ErrCircularDependency)
	return ok
}

func (err ErrCircularDependency) Error() string {
	return fmt.Sprintf("circular dependencies exists [issue id: %d, dependency id: %d, type: %s]", err.IssueID, err.DependencyID, err.DependencyType)
}

// ErrDependenciesLeft represents an error where the issue you're trying to close is still blocked by open dependencies.
type ErrBlockingDependenciesLeft struct {
	IssueID int64
}

// IsErrDependenciesLeft checks if an error is a ErrDependenciesLeft.
func IsErrBlockingDependenciesLeft(err error) bool {
	_, ok := err.(ErrBlockingDependenciesLeft)
	return ok
}

func (err ErrBlockingDependenciesLeft) Error() string {
	return fmt.Sprintf("issue is blocked by open issues [issue id: %d]", err.IssueID)
}

// ErrUnknownDependencyType represents an error where an unknown dependency type was passed
type ErrUnknownDependencyType struct {
	Type DependencyType
}

// IsErrUnknownDependencyType checks if an error is ErrUnknownDependencyType
func IsErrUnknownDependencyType(err error) bool {
	_, ok := err.(ErrUnknownDependencyType)
	return ok
}

func (err ErrUnknownDependencyType) Error() string {
	return fmt.Sprintf("unknown dependency type [type: %d]", err.Type)
}

func (err ErrUnknownDependencyType) Unwrap() error {
	return util.ErrInvalidArgument
}

// IssueDependency represents an issue dependency
type IssueDependency struct {
	ID           int64              `xorm:"pk autoincr"`
	UserID       int64              `xorm:"NOT NULL"`
	IssueID      int64              `xorm:"UNIQUE(issue_dependency) NOT NULL"`
	DependencyID int64              `xorm:"UNIQUE(issue_dependency) NOT NULL"`
	Type         DependencyType     `xorm:"UNIQUE(issue_dependency) NOT NULL"`
	CreatedUnix  timeutil.TimeStamp `xorm:"created"`
	UpdatedUnix  timeutil.TimeStamp `xorm:"updated"`
}

func init() {
	db.RegisterModel(new(IssueDependency))
}

// DependencyType Defines Dependency Type Constants
type DependencyType int

// Define Dependency Types
const (
	DependencyTypeBlockedBy DependencyType = iota
	DependencyTypeBlocking
	DependencyTypeCausedBy
	DependencyTypeCausing
	DependencyTypeRelatedTo
	DependencyTypeRelating
	DependencyTypeDuplicatedBy
	DependencyTypeDuplicating
)

// CreateIssueDependency creates a new dependency for an issue
func CreateIssueDependency(ctx context.Context, user *user_model.User, issue, dep *Issue, depType DependencyType) error {
	ctx, committer, err := db.TxContext(ctx)
	if err != nil {
		return err
	}
	defer committer.Close()

	depTypeStr := MapDependencyTypeToStr(depType)

	// Check if it already exists
	exists, err := issueDepExists(ctx, issue.ID, dep.ID, depType)
	if err != nil {
		return err
	}
	if exists {
		return ErrDependencyExists{issue.ID, dep.ID, depTypeStr}
	}
	// And if it would be circular
	circular, err := issueDepExists(ctx, dep.ID, issue.ID, depType)
	if err != nil {
		return err
	}
	if circular {
		return ErrCircularDependency{issue.ID, dep.ID, depTypeStr}
	}

	if err := db.Insert(ctx, &IssueDependency{
		UserID:       user.ID,
		IssueID:      issue.ID,
		DependencyID: dep.ID,
		Type:         depType,
	}); err != nil {
		return err
	}

	// Add comment referencing the new dependency
	if err = createIssueDependencyComment(ctx, user, issue, dep, true); err != nil {
		return err
	}

	return committer.Commit()
}

// RemoveIssueDependency removes a dependency from an issue
func RemoveIssueDependency(ctx context.Context, user *user_model.User, issue, dep *Issue, depType DependencyType) (err error) {
	ctx, committer, err := db.TxContext(ctx)
	if err != nil {
		return err
	}
	defer committer.Close()

	var issueDepToDelete IssueDependency

	switch depType {
	case DependencyTypeBlockedBy, DependencyTypeCausedBy, DependencyTypeDuplicatedBy, DependencyTypeRelatedTo:
		issueDepToDelete = IssueDependency{IssueID: issue.ID, DependencyID: dep.ID, Type: depType}
	case DependencyTypeBlocking, DependencyTypeCausing, DependencyTypeDuplicating, DependencyTypeRelating:
		issueDepToDelete = IssueDependency{IssueID: dep.ID, DependencyID: issue.ID, Type: MapReverseDependencyType(depType)}
	default:
		return ErrUnknownDependencyType{depType}
	}

	affected, err := db.GetEngine(ctx).Delete(&issueDepToDelete)
	if err != nil {
		return err
	}

	// If we deleted nothing, the dependency did not exist
	if affected <= 0 {
		return ErrDependencyNotExists{issue.ID, dep.ID, MapDependencyTypeToStr(depType)}
	}

	// Add comment referencing the removed dependency
	if err = createIssueDependencyComment(ctx, user, issue, dep, false); err != nil {
		return err
	}
	return committer.Commit()
}

// Check if the dependency already exists
func issueDepExists(ctx context.Context, issueID, depID int64, depType DependencyType) (bool, error) {
	return db.GetEngine(ctx).Where("(issue_id = ? AND dependency_id = ? AND type = ?)", issueID, depID, depType).Exist(&IssueDependency{})
}

// IssueNoDependenciesLeft checks if issue can be closed
func IssueNoBlockingDependenciesLeft(ctx context.Context, issue *Issue) (bool, error) {
	exists, err := db.GetEngine(ctx).
		Table("issue_dependency").
		Select("issue.*").
		Join("INNER", "issue", "issue.id = issue_dependency.dependency_id").
		Where("issue_dependency.issue_id = ?", issue.ID).
		And("issue.is_closed = ?", "0").
		And("issue_dependency.type = ?", DependencyTypeBlockedBy).
		Exist(&Issue{})

	return !exists, err
}

func MapStrToDependencyType(depTypeStr string) DependencyType {
	switch depTypeStr {
	case "blockedBy":
		return DependencyTypeBlockedBy
	case "blocking":
		return DependencyTypeBlocking
	case "causedBy":
		return DependencyTypeCausedBy
	case "causing":
		return DependencyTypeCausing
	case "duplicatedBy":
		return DependencyTypeDuplicatedBy
	case "duplicating":
		return DependencyTypeDuplicating
	case "relatedTo":
		return DependencyTypeRelatedTo
	case "relating":
		return DependencyTypeRelating
	default:
		return -1
	}
}

func MapDependencyTypeToStr(depType DependencyType) string {
	switch depType {
	case DependencyTypeBlockedBy:
		return "blockedBy"
	case DependencyTypeBlocking:
		return "blocking"
	case DependencyTypeCausedBy:
		return "causedBy"
	case DependencyTypeCausing:
		return "causing"
	case DependencyTypeDuplicatedBy:
		return "duplicatedBy"
	case DependencyTypeDuplicating:
		return "duplicating"
	case DependencyTypeRelatedTo:
		return "relatedTo"
	case DependencyTypeRelating:
		return "relating"
	default:
		return ""
	}
}

func MapReverseDependencyType(depType DependencyType) DependencyType {
	switch depType {
	case DependencyTypeBlocking:
		return DependencyTypeBlockedBy
	case DependencyTypeCausing:
		return DependencyTypeCausedBy
	case DependencyTypeDuplicating:
		return DependencyTypeDuplicatedBy
	case DependencyTypeRelating:
		return DependencyTypeRelatedTo
	default:
		return depType
	}
}
