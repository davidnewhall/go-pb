// Copyright 2021 Ilia Frenkel. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE.txt file.

// Package sqldb provides implementation of api.PasteService that uses
// a database as a storage.
package sqldb

import (
	"errors"
	"fmt"
	"math/rand"
	"strconv"
	"time"

	"github.com/iliafrenkel/go-pb/src/api"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// SvcOptions contains all the options needed to create an instance
// of PasteService
type SvcOptions struct {
	// Database connection string.
	// For sqlite it should be either a file name or `file::memory:?cache=shared`
	// to use temporary database in memory (ex. for testing).
	DBConnection *gorm.DB
	//
	DBAutoMigrate bool
}

// PasteService stores all the pastes in a database and implements the
// api.PasteService interface.
type PasteService struct {
	db      *gorm.DB
	Options SvcOptions
}

// New returns new PasteService with an empty map of pastes.
func New(opts SvcOptions) (*PasteService, error) {
	var s PasteService
	s.Options = opts
	db := opts.DBConnection
	rand.Seed(time.Now().UnixNano())

	if s.Options.DBAutoMigrate {
		db.AutoMigrate(&api.Paste{})
	}
	s.db = db

	return &s, nil
}

// Get returns a paste by it's ID.
// The return values are as follows:
// - if there is a problem talking to the database paste== nil, err != nil
// - if paste is not found paste== nil, err == nil
// - if paste is found paste != nil, err == nil
func (s *PasteService) Get(id int64) (*api.Paste, error) {
	if s.db == nil {
		return nil, errors.New("Get: no database connection")
	}
	var paste api.Paste
	err := s.db.Joins("User").First(&paste, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("Get: database error: %w", err)
	}

	return &paste, nil
}

// Create initialises a new paste from the provided data and adds it to the
// storage. It returns the newly created paste.
func (s *PasteService) Create(p api.PasteForm) (*api.Paste, error) {
	var (
		expires, created time.Time
	)
	created = time.Now()
	expires = time.Time{} // zero time means no expiration, this is the default
	// We expect the expiration to be in the form of "nx" where "n" is a number
	// and "x" is a time unit character: m for minute, h for hour, d for day,
	// w for week, M for month and y for year.
	if p.Expires != "never" && len(p.Expires) > 1 {
		dur, err := strconv.Atoi(p.Expires[:len(p.Expires)-1])
		if err != nil {
			return nil, fmt.Errorf("wrong duration format: %s, error: %w", p.Expires, err)
		}
		switch p.Expires[len(p.Expires)-1] {
		case 'm': //minutes
			expires = created.Add(time.Duration(dur) * time.Minute)
		case 'h': //hours
			expires = created.Add(time.Duration(dur) * time.Hour)
		case 'd': //days
			expires = created.AddDate(0, 0, dur)
		case 'w': //weeks
			expires = created.AddDate(0, 0, dur*7)
		case 'M': //months
			expires = created.AddDate(0, dur, 0)
		case 'y': //days
			expires = created.AddDate(dur, 0, 0)
		default:
			return nil, fmt.Errorf("unknown duration format: %s", p.Expires)
		}
	}
	// Create new paste with a randomly generated ID and a hashed password.
	if p.Password != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(p.Password), bcrypt.DefaultCost)
		if err != nil {
			return nil, err
		}
		p.Password = string(hash)
	}
	newPaste := api.Paste{
		ID:              rand.Int63(),
		Title:           p.Title,
		Body:            p.Body,
		Expires:         expires,
		DeleteAfterRead: p.DeleteAfterRead,
		Privacy:         p.Privacy,
		Password:        p.Password,
		Created:         created,
		Syntax:          p.Syntax,
		UserID:          p.UserID,
	}
	var err error
	if p.UserID == 0 {
		err = s.db.Omit("user_id").Create(&newPaste).Error
	} else {
		err = s.db.Create(&newPaste).Error
	}
	if err != nil {
		return nil, err
	}

	return &newPaste, nil
}

// Delete removes the paste from the storage
func (s *PasteService) Delete(id int64) error {
	return s.db.Delete(&api.Paste{}, id).Error
}

// List returns a slice of all the pastes by a user ID.
func (s *PasteService) List(uid int64) []api.Paste {
	var pastes []api.Paste

	if uid == 0 {
		s.db.Find(&pastes, "user_id IS NULL")
	} else {
		s.db.Find(&pastes, "user_id=?", uid)
	}

	return pastes
}
