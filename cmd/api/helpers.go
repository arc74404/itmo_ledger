package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/julienschmidt/httprouter"
)

func (app *application) readIDParam(r *http.Request) (uuid.UUID, error) {
	params := httprouter.ParamsFromContext(r.Context())

	id, err := uuid.Parse(params.ByName("id"))
	if err != nil {
		return uuid.Nil, errors.New("invalid id param")
	}

	return id, nil
}

func (app *application) writeJSON(w http.ResponseWriter, status int, data any, headers http.Header) error {
	js, err := json.Marshal(data)
	if err != nil {
		return err
	}

	js = append(js, '\n')

	for header, value := range headers {
		w.Header()[header] = value
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if _, err = w.Write(js); err != nil {
		return err
	}

	return nil
}

// errors handling with map

type jsonDecodeHandler func(error) (bool, error)

var jsonDecodeHandlers = []jsonDecodeHandler{
	func(err error) (bool, error) {
		var syntaxError *json.SyntaxError
		if errors.As(err, &syntaxError) {
			return true, fmt.Errorf("body contains badly-formed JSON (at character %d)", syntaxError.Offset)
		}
		return false, nil
	},
	func(err error) (bool, error) {
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return true, errors.New("body contains badly-formed JSON")
		}
		return false, nil
	},
	func(err error) (bool, error) {
		var unmarshalTypeError *json.UnmarshalTypeError
		if errors.As(err, &unmarshalTypeError) {
			if unmarshalTypeError.Field != "" {
				return true, fmt.Errorf("body contains incorrect JSON type for field %q", unmarshalTypeError.Field)
			}
			return true, fmt.Errorf("body contains incorrect JSON type (at character %d)", unmarshalTypeError.Offset)
		}
		return false, nil
	},
	func(err error) (bool, error) {
		if errors.Is(err, io.EOF) {
			return true, errors.New("body must not be empty")
		}
		return false, nil
	},
	func(err error) (bool, error) {
		if strings.HasPrefix(err.Error(), "json: unknown field ") {
			fieldName := strings.TrimPrefix(err.Error(), "json: unknown field ")
			return true, fmt.Errorf("body contains unknown key %s", fieldName)
		}
		return false, nil
	},
	func(err error) (bool, error) {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			return true, fmt.Errorf("body must not be larger than %d bytes", maxBytesError.Limit)
		}
		return false, nil
	},
	func(err error) (bool, error) {
		var invalidUnmarshalError *json.InvalidUnmarshalError
		if errors.As(err, &invalidUnmarshalError) {
			panic(err)
		}
		return false, nil
	},
}

func handleJSONDecodeError(err error) error {
	for _, handler := range jsonDecodeHandlers {
		if matched, handledErr := handler(err); matched {
			return handledErr
		}
	}
	return err
}

func (app *application) readJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	maxBytes := 10 * 1024 // 10 Kb
	r.Body = http.MaxBytesReader(w, r.Body, int64(maxBytes))

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		return handleJSONDecodeError(err)
	}

	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return errors.New("body must only contain a single JSON value")
	}
	return nil
}
