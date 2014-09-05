// TODO(kash): Rewrite this to use the new dir/object store api.
// +build ignore

package impl

import (
	"bytes"
	"testing"

	"veyron2/storage"
)

type mockQueryResult struct {
	name   string
	value  interface{}
	fields map[string]interface{}
}

func (mqr mockQueryResult) Name() string {
	return mqr.name
}

func (mqr mockQueryResult) Value() interface{} {
	return mqr.value
}

func (mqr mockQueryResult) Fields() map[string]interface{} {
	return mqr.fields
}

type mockQueryStream struct {
	index   int
	results []mockQueryResult
	error   error
}

func (mqs *mockQueryStream) Advance() bool {
	if mqs.error != nil {
		return false
	}
	// Initialize index to -1
	mqs.index++
	if mqs.index >= len(mqs.results) {
		return false
	}
	return true
}

func (mqs *mockQueryStream) Value() storage.QueryResult {
	return mqs.results[mqs.index]
}

func (mqs *mockQueryStream) Err() error {
	return mqs.error
}

func (mqs *mockQueryStream) Cancel() {
	mqs.index = len(mqs.results) + 1
}

type testCase struct {
	result         mockQueryResult
	expectedOutput string
}

const (
	result3Out = `result3: map[string]interface {}{
	qs: {
		resultNested1: 10
		resultNested2: 11
	},
},
`
)

func TestPrintResult(t *testing.T) {
	tests := []testCase{
		{
			result: mockQueryResult{
				name:   "result1",
				value:  10,
				fields: nil,
			},
			expectedOutput: "result1: 10\n",
		},

		{
			result: mockQueryResult{
				name:   "result2",
				value:  nil,
				fields: map[string]interface{}{"a": 1, "b": 2},
			},
			expectedOutput: `result2: map[string]interface {}{
	"a":1,
	"b":2,
},
`,
		},

		{
			result: mockQueryResult{
				name:  "result3",
				value: nil,
				fields: map[string]interface{}{
					"qs": storage.QueryStream(&mockQueryStream{
						index: -1,
						error: nil,
						results: []mockQueryResult{
							mockQueryResult{
								name:   "resultNested1",
								value:  10,
								fields: nil,
							},
							mockQueryResult{
								name:   "resultNested2",
								value:  11,
								fields: nil,
							},
						},
					}),
				},
			},
			expectedOutput: result3Out,
		},
	}

	for _, d := range tests {
		var b bytes.Buffer
		printResult(d.result, &b, 0)

		if got, want := b.String(), d.expectedOutput; got != want {
			t.Errorf("got <%s>, want <%s>", got, want)
		}
	}
}
