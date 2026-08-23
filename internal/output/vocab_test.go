package output

import (
	"encoding/json"
	"testing"
)

func TestFailureCodeForStatus(t *testing.T) {
	cases := []struct {
		status int
		want   FailureCode
	}{
		{400, CodeInvalid},
		{422, CodeInvalid},
		{404, CodeNotFound},
		{409, CodeConflict},
		{412, CodePreconditionFailed},
		{413, CodeTooLarge},
		{429, CodeRateLimited},
		{503, CodeRateLimited},
		{500, CodeServerError},
		{502, CodeServerError},
		{0, CodeUnknown},
		{418, CodeUnknown}, // an unmapped status classifies as unknown
	}
	for _, c := range cases {
		if got := FailureCodeForStatus(c.status); got != c.want {
			t.Errorf("FailureCodeForStatus(%d) = %q, want %q", c.status, got, c.want)
		}
	}
}

// The unified Failure marshals code and message always, and httpStatus only when
// a status is present (0 is omitted). This is the frozen schema-3 shape.
func TestFailureJSONShape(t *testing.T) {
	withStatus, err := json.Marshal(Failure{Code: CodePreconditionFailed, HTTPStatus: 412, Message: "changed concurrently"})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(withStatus); got != `{"code":"precondition-failed","httpStatus":412,"message":"changed concurrently"}` {
		t.Errorf("with status = %s", got)
	}

	noStatus, err := json.Marshal(Failure{Code: CodeStagedFileFailed, Message: "could not rewind"})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(noStatus); got != `{"code":"staged-file-failed","message":"could not rewind"}` {
		t.Errorf("without status = %s (httpStatus must be omitted when zero)", got)
	}
}
