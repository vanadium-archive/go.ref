// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type cookieBaker interface {
	set(req *http.Request, w http.ResponseWriter, name, payload, csrfToken string) error
	get(req *http.Request, name, csrfToken string) (string, error)
}

type signedCookieBaker struct {
	secure   bool
	signKey  string
	validity time.Duration
}

type signedCookie struct {
	// Name of the cookie.
	Name string `json:"name"`
	// Payload is the value the cookie is meant to keep.
	Payload string `json:"payload"`
	// CSRFToken prevents CSRF attacks by ensuring the cookie matches a GET
	// request param.
	CSRFToken string `json:"csrfToken"`
	// Expiry ensures cookies cannot be used beyond their intended validity.
	Expiry time.Time `json:"expiry"`
	// HMAC signs all the above fields and prevents tampering with the
	// cookie.
	HMAC []byte `json:"hmac"`
}

func (c *signedCookie) computeHMAC(signKey string) []byte {
	mac := hmac.New(sha256.New, []byte(signKey))
	mac.Write([]byte(c.Name))
	mac.Write([]byte(c.Payload))
	mac.Write([]byte(c.CSRFToken))
	mac.Write([]byte(c.Expiry.String()))
	return mac.Sum(nil)
}

func (c *signedCookie) verifyHMAC(signKey string) bool {
	return hmac.Equal(c.HMAC, c.computeHMAC(signKey))
}

func (b *signedCookieBaker) packCookie(name, payload, csrfToken string) (string, error) {
	c := signedCookie{
		Name:      name,
		Payload:   payload,
		CSRFToken: csrfToken,
		Expiry:    time.Now().Add(b.validity),
	}
	c.HMAC = c.computeHMAC(b.signKey)
	jsonData, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	base64Data := make([]byte, base64.URLEncoding.EncodedLen(len(jsonData)))
	base64.URLEncoding.Encode(base64Data, jsonData)
	return string(base64Data), nil
}

func (b *signedCookieBaker) unpackCookie(value, csrfToken string) (string, error) {
	jsonData := make([]byte, base64.URLEncoding.DecodedLen(len(value)))
	if n, err := base64.URLEncoding.Decode(jsonData, []byte(value)); err != nil {
		return "", err
	} else {
		jsonData = jsonData[:n]
	}
	var c signedCookie
	if err := json.Unmarshal(jsonData, &c); err != nil {
		return "", err
	}
	if time.Now().After(c.Expiry) {
		return "", fmt.Errorf("cookie expired on %v", c.Expiry)
	}
	if !c.verifyHMAC(b.signKey) {
		return "", fmt.Errorf("HMAC mismatching for cookie %v", c)
	}
	if c.CSRFToken != csrfToken {
		return "", fmt.Errorf("CSRF token mismatching for cookie %v", c)
	}
	return c.Payload, nil
}

func (b *signedCookieBaker) set(req *http.Request, w http.ResponseWriter, name, payload, csrfToken string) error {
	cookieValue, err := b.packCookie(name, payload, csrfToken)
	if err != nil {
		return err
	}
	cookie := http.Cookie{
		Name:     name,
		Value:    cookieValue,
		Expires:  time.Now().Add(b.validity),
		HttpOnly: true,
		Secure:   b.secure,
		Path:     "/",
	}
	http.SetCookie(w, &cookie)
	return nil
}

func (b *signedCookieBaker) get(req *http.Request, name, csrfToken string) (string, error) {
	cookie, err := req.Cookie(name)
	if err == http.ErrNoCookie {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return b.unpackCookie(cookie.Value, csrfToken)
}
