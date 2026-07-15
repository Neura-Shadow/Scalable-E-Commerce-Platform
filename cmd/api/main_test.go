package main

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	dbMocks "goshop/pkg/dbs/mocks"
)

func TestMigrateDatabaseDoesNothingWhenAutoMigrateIsDisabled(t *testing.T) {
	database := dbMocks.NewIDatabase(t)

	err := migrateDatabase(database, false)

	assert.NoError(t, err)
}

func TestMigrateDatabaseRunsAllProductionModelsWhenEnabled(t *testing.T) {
	database := dbMocks.NewIDatabase(t)
	database.
		On("AutoMigrate", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil).
		Once()

	err := migrateDatabase(database, true)

	assert.NoError(t, err)
}

func TestMigrateDatabaseReturnsMigrationFailure(t *testing.T) {
	database := dbMocks.NewIDatabase(t)
	expected := errors.New("migration failed")
	database.
		On("AutoMigrate", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(expected).
		Once()

	err := migrateDatabase(database, true)

	assert.ErrorIs(t, err, expected)
}
