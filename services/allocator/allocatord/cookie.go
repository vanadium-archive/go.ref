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
	set(w http.ResponseWriter, name, payload, csrfToken string) error
	get(req *http.Request, name, csrfToken string) (string, error)
}

type signedCookieBaker struct {
	secure   bool
	signKey  string
	validity time.Duration
}

type signedCookie struct {
	// Payload is the value the cookie is meant to keep.
	Payload string `json:"payload"`
	// Expiry ensures cookies cannot be used beyond their intended validity.
	Expiry time.Time `json:"expiry"`
	// HMAC signs all the above fields and prevents tampering with the
	// cookie.
	HMAC []byte `json:"hmac"`
}

func (c *signedCookie) computeHMAC(name, csrfToken, signKey string) []byte {
	mac := hmac.New(sha256.New, []byte(signKey))
	put := func(data string) {
		fmt.Fprintf(mac, "%08x%s", len(data), data)
	}
	put(name)
	put(csrfToken)
	put(c.Payload)
	put(c.Expiry.String())
	return mac.Sum(nil)
}

func (c *signedCookie) verifyHMAC(name, csrfToken, signKey string) bool {
	return hmac.Equal(c.HMAC, c.computeHMAC(name, csrfToken, signKey))
}

func (b *signedCookieBaker) packCookie(name, payload, csrfToken string) (string, error) {
	c := signedCookie{
		Payload: payload,
		Expiry:  time.Now().Add(b.validity),
	}
	c.HMAC = c.computeHMAC(name, csrfToken, b.signKey)
	jsonData, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(jsonData), nil
}

func (b *signedCookieBaker) unpackCookie(name, value, csrfToken string) (string, error) {
	jsonData, err := base64.URLEncoding.DecodeString(value)
	if err != nil {
		return "", err
	}
	var c signedCookie
	if err := json.Unmarshal(jsonData, &c); err != nil {
		return "", err
	}
	if time.Now().After(c.Expiry) {
		return "", fmt.Errorf("cookie expired on %v", c.Expiry)
	}
	if !c.verifyHMAC(name, csrfToken, b.signKey) {
		return "", fmt.Errorf("HMAC mismatching for cookie %v", c)
	}
	return c.Payload, nil
}

func (b *signedCookieBaker) set(w http.ResponseWriter, name, payload, csrfToken string) error {
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
		Path:     routeRoot,
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
	return b.unpackCookie(name, cookie.Value, csrfToken)
}
