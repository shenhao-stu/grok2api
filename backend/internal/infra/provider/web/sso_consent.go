package web

import (
	"bytes"
	"fmt"
	"io"
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

// Parse only the official approval form; never manufacture its authorization token.
func parseSSOConsentForm(body []byte, userCode string) (url.Values, error) {
	invalid := func() (url.Values, error) {
		return nil, fmt.Errorf("xAI Device Flow 授权表单缺失、无效或已改变")
	}
	fields := url.Values{}
	tokens := html.NewTokenizer(bytes.NewReader(body))
	inForm, found, allow := false, false, false
	for {
		switch tokens.Next() {
		case html.ErrorToken:
			if tokens.Err() != io.EOF {
				return invalid()
			}
			if !found || !allow || fields.Get("user_code") != userCode || fields.Get("consent_token") == "" || fields.Get("principal_type") != "User" {
				return invalid()
			}
			fields.Set("action", "allow")
			return fields, nil
		case html.EndTagToken:
			if tokens.Token().Data == "form" {
				inForm = false
			}
		case html.StartTagToken, html.SelfClosingTagToken:
			token := tokens.Token()
			attrs := map[string]string{}
			for _, attr := range token.Attr {
				if _, exists := attrs[attr.Key]; exists {
					return invalid()
				}
				attrs[attr.Key] = attr.Val
			}
			if token.Data == "form" {
				inForm = attrs["action"] == ssoApproveURL && strings.EqualFold(attrs["method"], "post")
				if inForm {
					if found {
						return invalid()
					}
					found = true
				}
			}
			if !inForm {
				continue
			}
			if token.Data == "button" && attrs["name"] == "action" && attrs["value"] == "allow" {
				allow = true
			}
			if token.Data != "input" || attrs["type"] != "hidden" {
				continue
			}
			switch name := attrs["name"]; name {
			case "user_code", "principal_type", "principal_id", "consent_token", "castle_request_token":
				if _, exists := fields[name]; exists || len(attrs["value"]) > 16384 {
					return invalid()
				}
				fields.Set(name, attrs["value"])
			}
		}
	}
}
