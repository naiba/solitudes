package akismet

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"reflect"
	"strings"
)

// All Akismet endpoints have the exact same request body format, with only
// the endpoint changing between comment checks and submitting ham/spam.
// Therefore, it makes sense to abstract the HTTP request in an unexported
// function and re-use everywhere.
func postRequest(c *Comment, key string, endpoint string) (string, error) {
	form := url.Values{}

	v := reflect.ValueOf(*c)
	t := v.Type()
	for i := 0; i < v.Type().NumField(); i++ {
		if v.Field(i).String() != "" {
			form.Add(t.Field(i).Tag.Get("form"), v.Field(i).String())
		}
	}

	client := &http.Client{}

	reqBody := strings.NewReader(form.Encode())
	api := fmt.Sprintf("https://%s.rest.akismet.com/1.1/%s", key, endpoint)
	req, err := http.NewRequest("POST", api, reqBody)
	if err != nil {
		return "", err
	}

	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}

	defer resp.Body.Close()
	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(respBody), nil
}
