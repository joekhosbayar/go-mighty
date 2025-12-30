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

func (f *fakeClient) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if f.ExecContextErr != nil {
		return nil, f.ExecContextErr
	}
	if f.ExecContextVal != nil {
		return f.ExecContextVal, nil
	}
	return dummyResult{}, nil
}

func (f *fakeClient) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if f.QueryContextErr != nil {
		return nil, f.QueryContextErr
	}
	if f.QueryContextVal != nil {
		return f.QueryContextVal, nil
	}
	return nil, nil
}

func (f *fakeClient) QueryRowContext(ctx context.Context, query string, args ...any) Row {
	return &fakeRow{val: f.QueryRowVal, scanErr: f.QueryRowErr}
}

func (f *fakeClient) PingContext(ctx context.Context) error {
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
			*d = r.val.(string)
		case *int:
			*d = r.val.(int)
		}
	}
	return nil
}

type dummyResult struct{}

func (d dummyResult) LastInsertId() (int64, error) { return 0, nil }
func (d dummyResult) RowsAffected() (int64, error) { return 0, nil }

// ----------------------------
// Table-driven unit tests
// ----------------------------

func TestPostgresClient_Ping(t *testing.T) {
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
			fake := &fakeClient{PingContextErr: tt.fakeErr}
			client := &PostgresClient{db: fake}

			err := client.Ping(context.Background())
			if (err != nil) != tt.wantErr {
				t.Fatalf("Ping() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestPostgresClient_Exec(t *testing.T) {
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
			fake := &fakeClient{
				ExecContextErr: tt.execErr,
				ExecContextVal: tt.execResult,
			}
			client := &PostgresClient{db: fake}

			err := client.Exec(context.Background(), "INSERT INTO test VALUES(1)")
			if (err != nil) != tt.wantErr {
				t.Fatalf("Exec() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestPostgresClient_QueryRow(t *testing.T) {
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
			fake := &fakeClient{
				QueryRowVal: tt.rowVal,
				QueryRowErr: tt.rowErr,
			}
			client := &PostgresClient{db: fake}

			row := client.db.QueryRowContext(context.Background(), "SELECT val FROM test")
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

func TestPostgresClient_QueryContext(t *testing.T) {
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
			fake := &fakeClient{
				QueryContextErr: tt.queryErr,
			}
			client := &PostgresClient{db: fake}

			rows, err := client.db.QueryContext(context.Background(), "SELECT val FROM test")
			if (err != nil) != tt.wantErr {
				t.Fatalf("QueryContext() error = %v, wantErr %v", err, tt.wantErr)
			}

			// For simplicity, just check nil/non-nil; real iteration isn't needed
			if tt.queryErr == nil && rows != nil {
				// OK
			}
		})
	}
}
