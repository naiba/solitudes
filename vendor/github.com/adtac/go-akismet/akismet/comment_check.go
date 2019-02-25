package akismet

import (
	"errors"
)

// This function checks whether a particular Comment is spam or not by querying
// the Akismet API. The returned boolean is true if the comment was classified
// as spam, false otherwise. If the request failed for whatever reason, the
// error returned will be non-nil.
func Check(c *Comment, key string) (bool, error) {
	respBody, err := postRequest(c, key, "comment-check")
	if err != nil {
		return true, err
	}

	if respBody == "true" {
		return true, nil
	} else if respBody == "false" {
		return false, nil
	}

	return true, errors.New(respBody)
}
