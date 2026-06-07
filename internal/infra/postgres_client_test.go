package infra

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

// ----------------------------
// fakeClient for testing
// ----------------------------

type fakeClient struct {
	ExecContextErr  error
	QueryContextErr error
	PingContextErr  error
	ExecContextVal  sql.Result
	QueryContextVal *sql.Rows
	QueryRowVal     any
	QueryRowErr     error
}

func (f *fakeClient) ExecContext(_ context.Context, _ string, _ ...any) (sql.Result, error) {
	if f.ExecContextErr != nil {
		return nil, f.ExecContextErr
	}

	if f.ExecContextVal != nil {
		return f.ExecContextVal, nil
	}

	return dummyResult{}, nil
}

func (f *fakeClient) QueryContext(_ context.Context, _ string, _ ...any) (*sql.Rows, error) {
	if f.QueryContextErr != nil {
		return nil, f.QueryContextErr
	}

	if f.QueryContextVal != nil {
		return f.QueryContextVal, nil
	}

	return nil, nil
}

func (f *fakeClient) QueryRowContext(_ context.Context, _ string, _ ...any) Row {
	return &fakeRow{val: f.QueryRowVal, scanErr: f.QueryRowErr}
}

func (f *fakeClient) PingContext(_ context.Context) error {
	return f.PingContextErr
}

// ----------------------------
// fakeRow and dummyResult
// ----------------------------

type fakeRow struct {
	val     any
	scanErr error
}

func (r *fakeRow) Scan(dest ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}

	if len(dest) > 0 && r.val != nil {
		switch d := dest[0].(type) {
		case *string:
			if v, ok := r.val.(string); ok {
				*d = v
			}
		case *int:
			if v, ok := r.val.(int); ok {
				*d = v
			}
		}
	}

	return nil
}

type dummyResult struct{}

func (_ dummyResult) LastInsertId() (int64, error) { return 0, nil }
func (_ dummyResult) RowsAffected() (int64, error) { return 0, nil }

// ----------------------------
// Table-driven unit tests
// ----------------------------

func TestPostgres_Ping(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		fakeErr error
		wantErr bool
	}{
		{"PingSuccess", nil, false},
		{"PingFailure", errors.New("ping failed"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fake := &fakeClient{PingContextErr: tt.fakeErr}
			client := &Postgres{db: fake}

			err := client.Ping(t.Context())
			if (err != nil) != tt.wantErr {
				t.Fatalf("Ping() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestPostgres_Exec(t *testing.T) {
	t.Parallel()
	dummyRes := dummyResult{}
	tests := []struct {
		name       string
		execErr    error
		execResult sql.Result
		wantErr    bool
	}{
		{"ExecSuccess", nil, dummyRes, false},
		{"ExecFailure", errors.New("exec failed"), nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fake := &fakeClient{
				ExecContextErr: tt.execErr,
				ExecContextVal: tt.execResult,
			}
			client := &Postgres{db: fake}

			err := client.Exec(t.Context(), "INSERT INTO test VALUES(1)")
			if (err != nil) != tt.wantErr {
				t.Fatalf("Exec() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestPostgres_QueryRow(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		rowVal      any
		rowErr      error
		wantScanErr bool
		expectedVal string
	}{
		{"QueryRowSuccess", "hello", nil, false, "hello"},
		{"QueryRowFailure", nil, errors.New("scan failed"), true, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fake := &fakeClient{
				QueryRowVal: tt.rowVal,
				QueryRowErr: tt.rowErr,
			}
			client := &Postgres{db: fake}

			row := client.db.QueryRowContext(t.Context(), "SELECT val FROM test")

			var val string

			err := row.Scan(&val)
			if (err != nil) != tt.wantScanErr {
				t.Fatalf("Scan() error = %v, wantScanErr %v", err, tt.wantScanErr)
			}

			if val != tt.expectedVal {
				t.Fatalf("Scan() value = %v, expected %v", val, tt.expectedVal)
			}
		})
	}
}

func TestPostgres_QueryContext(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		queryErr error
		wantErr  bool
	}{
		{"QueryContextSuccess", nil, false},
		{"QueryContextFailure", errors.New("query failed"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fake := &fakeClient{
				QueryContextErr: tt.queryErr,
			}
			client := &Postgres{db: fake}

			rows, err := client.db.QueryContext(t.Context(), "SELECT val FROM test")
			if (err != nil) != tt.wantErr {
				t.Fatalf("QueryContext() error = %v, wantErr %v", err, tt.wantErr)
			}
			if rows != nil {
				defer func() { _ = rows.Close() }()
				_ = rows.Err()
			}

			// For simplicity, just check nil/non-nil; real iteration isn't needed
			if tt.queryErr == nil && rows != nil {
				// OK
			}
		})
	}
}
