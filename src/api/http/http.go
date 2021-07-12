package http

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/iliafrenkel/go-pb/src/api"
	"github.com/iliafrenkel/go-pb/src/api/base62"
)

// ApiServer type provides an HTTP server that calls PasteService methods in
// response to HTTP requests to certain routes.
//
// Use the `New` function to create an instance of ApiServer with the default
// routes.
type ApiServer struct {
	PasteService api.PasteService
	Router       *gin.Engine
	Server       *http.Server
}

// New function returns an instance of ApiServer using provided PasteService
// and the default HTTP routes for manipulating pastes.
//
// The routes are:
//   GET    /paste/{id} - get paste by ID
//   POST   /paste      - create new paste
//   DELETE /paste/{id} - delete paste by ID
func New(svc api.PasteService) *ApiServer {
	var handler ApiServer

	handler.PasteService = svc
	handler.Router = gin.Default()
	handler.Router.GET("/paste/:id", handler.handlePaste)
	handler.Router.POST("/paste", handler.handleCreate)
	handler.Router.DELETE("/paste/:id", handler.handleDelete)

	return &handler
}

// ListenAndServe starts an HTTP server and binds it to the provided address.
//
// TODO: Timeouts should be configurable.
func (h *ApiServer) ListenAndServe(addr string) error {
	h.Server = &http.Server{
		Addr: addr,
		// Good practice to set timeouts to avoid Slowloris attacks.
		WriteTimeout: time.Second * 15,
		ReadTimeout:  time.Second * 15,
		IdleTimeout:  time.Second * 60,
		Handler:      h.Router,
	}

	log.Println("API server listening on ", addr)

	return h.Server.ListenAndServe()
}

// handlePaste is an HTTP handler for the GET /paste/{id} route, it returns
// the paste as a JSON string or 404 Not Found.
func (h *ApiServer) handlePaste(c *gin.Context) {
	// We expect the id parameter as base62 encoded string, we try to decode
	// it into a uint64 paste id and return 404 if we can't.
	id, err := base62.Decode(c.Param("id"))
	if err != nil {
		log.Println(err)
		c.String(http.StatusNotFound, "paste not found")
		return
	}

	p, err := h.PasteService.Paste(id)
	if err != nil {
		log.Println(err)
		c.String(http.StatusNotFound, "paste not found")
		return
	}

	c.JSON(http.StatusOK, p)

	// We "burn" the paste if DeleteAfterRead flag is set.
	if p.DeleteAfterRead {
		h.PasteService.Delete(p.ID)
	}
}

// handleCreate is an HTTP handler for the POST /paste route. It expects the
// new paste as a JSON sting in the body of the request. Returns newly created
// paste as a JSON string and the 'Location' header set to the new paste URL.
//
// The JSON object must correspond to the api.Paste struct. Absent fields will
// get default values. Extra fields will generate an error. Only one object is
// expected, multiple JSON objects in the body will result in an error. Body
// size is currently limited to a hardcoded value of 10KB.
//
// TODO: Make maximum body size configurable.
func (h *ApiServer) handleCreate(c *gin.Context) {
	// Parse incoming json
	// https://www.alexedwards.net/blog/how-to-properly-parse-a-json-request-body

	// If the Content-Type header is present, check that it has the value
	// application/json.
	if h := c.GetHeader("Content-Type"); h != "" {
		if h != "application/json" {
			c.String(http.StatusUnsupportedMediaType, "wrong Content-Type header, expect application/json")
			return
		}
	}

	// Use http.MaxBytesReader to enforce a maximum read of 10KB from the
	// response body. A request body larger than that will now result in
	// Decode() returning a "http: request body too large" error.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 10240)

	// Setup the decoder and call the DisallowUnknownFields() method on it.
	// This will cause Decode() to return a "json: unknown field ..." error
	// if it encounters any extra unexpected fields in the JSON. Strictly
	// speaking, it returns an error for "keys which do not match any
	// non-ignored, exported fields in the destination".
	dec := json.NewDecoder(c.Request.Body)
	dec.DisallowUnknownFields()

	var data api.Paste
	if err := dec.Decode(&data); err != nil {
		var syntaxError *json.SyntaxError
		var unmarshalTypeError *json.UnmarshalTypeError

		switch {
		// Catch any syntax errors in the JSON and send an error message
		// which interpolates the location of the problem to make it
		// easier for the client to fix.
		case errors.As(err, &syntaxError):
			msg := fmt.Sprintf("Request body contains malformed JSON (at position %d)", syntaxError.Offset)
			c.String(http.StatusBadRequest, msg)

		// In some circumstances Decode() may also return an
		// io.ErrUnexpectedEOF error for syntax errors in the JSON. There
		// is an open issue regarding this at
		// https://github.com/golang/go/issues/25956.
		case errors.Is(err, io.ErrUnexpectedEOF):
			msg := "Request body contains badly-formed JSON"
			c.String(http.StatusBadRequest, msg)

		// Catch any type errors, like trying to assign a string in the
		// JSON request body to a int field in our Paste struct. We can
		// interpolate the relevant field name and position into the error
		// message to make it easier for the client to fix.
		case errors.As(err, &unmarshalTypeError):
			msg := fmt.Sprintf("Request body contains an invalid value for the %q field (at position %d)", unmarshalTypeError.Field, unmarshalTypeError.Offset)
			c.String(http.StatusBadRequest, msg)

		// Catch the error caused by extra unexpected fields in the request
		// body. We extract the field name from the error message and
		// interpolate it in our custom error message. There is an open
		// issue at https://github.com/golang/go/issues/29035 regarding
		// turning this into a sentinel error.
		case strings.HasPrefix(err.Error(), "json: unknown field "):
			fieldName := strings.TrimPrefix(err.Error(), "json: unknown field ")
			msg := fmt.Sprintf("Request body contains unknown field %s", fieldName)
			c.String(http.StatusBadRequest, msg)

		// An io.EOF error is returned by Decode() if the request body is
		// empty.
		case errors.Is(err, io.EOF):
			msg := "Request body must not be empty"
			c.String(http.StatusBadRequest, msg)

		// Catch the error caused by the request body being too large. Again
		// there is an open issue regarding turning this into a sentinel
		// error at https://github.com/golang/go/issues/30715.
		case err.Error() == "http: request body too large":
			msg := "Request body must not be larger than 10KB"
			c.String(http.StatusBadRequest, msg)

		// Otherwise default to logging the error and sending a 500 Internal
		// Server Error response.
		default:
			log.Println(err.Error())
			c.String(http.StatusBadRequest, http.StatusText(http.StatusInternalServerError))
		}
		return
	}
	// Call decode again, using a pointer to an empty anonymous struct as
	// the destination. If the request body only contained a single JSON
	// object this will return an io.EOF error. So if we get anything else,
	// we know that there is additional data in the request body.

	if err := dec.Decode(&struct{}{}); err != io.EOF {
		msg := "Request body must only contain a single JSON object"
		c.String(http.StatusBadRequest, msg)
		return
	}

	// Create new paste
	rand.Seed(time.Now().UnixNano())
	p := api.Paste{
		ID:              rand.Uint64(),
		Title:           data.Title,
		Body:            data.Body,
		Expires:         time.Time{},
		Created:         time.Now(),
		DeleteAfterRead: data.DeleteAfterRead,
		Syntax:          data.Syntax,
	}
	if err := h.PasteService.Create(&p); err != nil {
		log.Printf("failed to create paste: %v\n", err)
		c.String(http.StatusBadRequest, "failed to create paste")
		return
	}
	c.Header("Location", p.URL())
	c.JSON(http.StatusCreated, p)
}

// handleDelete is an HTTP handler for the DELETE /paste/{id} route. Deletes
// the paste by id and returns 200 OK or 404 Not Found.
func (h *ApiServer) handleDelete(c *gin.Context) {
	id, err := base62.Decode(c.Param("id"))
	if err != nil {
		c.String(http.StatusNotFound, "paste not found")
		return
	}

	if err := h.PasteService.Delete(id); err != nil {
		c.String(http.StatusNotFound, "paste not found")
		return
	}
}