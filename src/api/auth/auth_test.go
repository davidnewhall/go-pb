package auth

import (
	"os"
	"sync"
	"testing"

	"github.com/iliafrenkel/go-pb/src/api"
)

var usrSvc *UserService
var tokenSecret = "5TEdWbDmxZ2ASXcMinBYwGi66vHiU9rq"

type MockStore struct {
	users     map[int64]*api.User
	usersLock *sync.RWMutex
}

func (s MockStore) Store(usr api.User) error {
	s.usersLock.Lock()
	defer s.usersLock.Unlock()
	s.users[usr.ID] = &usr

	return nil
}
func (s MockStore) Find(usr api.User) (*api.User, error) {
	s.usersLock.RLock()
	defer s.usersLock.RUnlock()
	for _, u := range s.users {
		if usr.Username != "" && u.Username == usr.Username {
			return u, nil
		}
		if usr.Email != "" && u.Email == usr.Email {
			return u, nil
		}
		if usr.ID != 0 && u.ID == usr.ID {
			return u, nil
		}
	}

	return nil, nil
}

func TestMain(m *testing.M) {
	usrSvc = new(UserService)
	store := new(MockStore)
	store.users = make(map[int64]*api.User)
	store.usersLock = &sync.RWMutex{}
	usrSvc.UserStore = store

	os.Exit(m.Run())
}

func Test_CreateUser(t *testing.T) {
	t.Parallel()
	var usr = api.UserRegister{
		Username:   "test",
		Email:      "test@example.com",
		Password:   "12345",
		RePassword: "12345",
	}
	var id int64

	err := usrSvc.Create(usr)

	if err != nil {
		t.Errorf("Failed to create a user: %v", err)
	}

	// Check if we can find the user by username
	u, _ := usrSvc.UserStore.Find(api.User{Username: usr.Username})
	if u == nil {
		t.Errorf("Failed to find a user by username")
	} else {
		id = u.ID
	}
	// Check if we can find the user by email
	u, _ = usrSvc.UserStore.Find(api.User{Email: usr.Email})
	if u == nil {
		t.Errorf("Failed to find a user by email")
	}
	// Check if we can find the user by id
	u, _ = usrSvc.UserStore.Find(api.User{ID: id})
	if u == nil {
		t.Errorf("Failed to find a user by ID")
	}

	// Try to create with the same username but different email
	usr.Email = "another@example.com"
	err = usrSvc.Create(usr)
	if err == nil {
		t.Errorf("Succeeded to create a user with existing username")
	}
	// Try to create with the same email but different username
	usr.Email = "test@example.com"
	usr.Username = "test2"
	err = usrSvc.Create(usr)
	if err == nil {
		t.Errorf("Succeeded to create a user with existing email")
	}
}

func Test_CreateUserEmptyUsername(t *testing.T) {
	t.Parallel()
	var usr = api.UserRegister{
		Username:   "",
		Email:      "emptyusername@example.com",
		Password:   "12345",
		RePassword: "12345",
	}
	err := usrSvc.Create(usr)
	if err == nil {
		t.Errorf("Succeeded to create a user with empty username")
	}
}
func Test_CreateUserEmptyEmail(t *testing.T) {
	t.Parallel()
	var usr = api.UserRegister{
		Username:   "emptyemail",
		Email:      "",
		Password:   "12345",
		RePassword: "12345",
	}
	err := usrSvc.Create(usr)
	if err == nil {
		t.Errorf("Succeeded to create a user with empty email")
	}
}

func Test_CreateUserPasswordsDontMatch(t *testing.T) {
	t.Parallel()
	var usr = api.UserRegister{
		Username:   "nonmatchingpasswords",
		Email:      "nonmatchingpasswords@example.com",
		Password:   "12345",
		RePassword: "54321",
	}
	err := usrSvc.Create(usr)
	if err == nil {
		t.Errorf("Succeeded to create a user with non-matching passwords")
	}
}

func Test_AuthenticateUser(t *testing.T) {
	t.Parallel()
	var usr = api.UserRegister{
		Username:   "auth",
		Email:      "auth@example.com",
		Password:   "12345",
		RePassword: "12345",
	}
	err := usrSvc.Create(usr)
	if err != nil {
		t.Errorf("Failed to create a user: %v", err)
	}

	var usrLogin = api.UserLogin{
		Username: usr.Username,
		Password: usr.Password,
	}

	inf, err := usrSvc.Authenticate(usrLogin, tokenSecret)

	if err != nil {
		t.Errorf("Failed to authenticate a user: %v", err)
	}

	if err == nil && inf.Token == "" {
		t.Errorf("Failed to authenticate a user: error is nil but token is empty")
	}

	//user doesn't exist
	usrLogin = api.UserLogin{
		Username: "idontexist",
		Password: "idontmatter",
	}

	_, err = usrSvc.Authenticate(usrLogin, tokenSecret)

	if err == nil {
		t.Errorf("Authentication succeeded for a user that doesn't exist: %#v", usrLogin)
	}

	//wrong password
	usrLogin = api.UserLogin{
		Username: usr.Username,
		Password: "wrong",
	}
	_, err = usrSvc.Authenticate(usrLogin, tokenSecret)

	if err == nil {
		t.Errorf("Authentication succeeded with incorrect password: %#v", usrLogin)
	}
}

func Test_ValidateToken(t *testing.T) {
	t.Parallel()
	var usr = api.UserRegister{
		Username:   "validate",
		Email:      "validate@example.com",
		Password:   "12345",
		RePassword: "12345",
	}
	err := usrSvc.Create(usr)
	if err != nil {
		t.Errorf("Failed to create a user: %v", err)
	}

	var usrLogin = api.UserLogin{
		Username: usr.Username,
		Password: usr.Password,
	}

	inf, err := usrSvc.Authenticate(usrLogin, tokenSecret)

	if err != nil {
		t.Errorf("Failed to authenticate a user: %v", err)
	}

	u, _ := usrSvc.UserStore.Find(api.User{Username: usr.Username})
	if u == nil {
		t.Errorf("Failed to find user by name: %v", u)
		return
	}

	v, err := usrSvc.Validate(*u, inf.Token, tokenSecret)
	if err != nil {
		t.Errorf("Failed to validate token: %v", err)
	}
	if v.Username == "" || v.Token == "" {
		t.Errorf("Token validation failed: %s - %#v", inf.Token, v)

	}
}