package mailcloak

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const DefaultConfigPath = "/etc/mailcloak/config.yaml"

type SetupStatus struct {
	ConfigPath          string `json:"config_path"`
	ConfigExists        bool   `json:"config_exists"`
	ConfigValid         bool   `json:"config_valid"`
	ConfigWritable      bool   `json:"config_writable"`
	ConfigError         string `json:"config_error,omitempty"`
	ConfigWritableError string `json:"config_writable_error,omitempty"`

	DBPath          string `json:"db_path"`
	DBExists        bool   `json:"db_exists"`
	DBInitialized   bool   `json:"db_initialized"`
	DBWritable      bool   `json:"db_writable"`
	DBError         string `json:"db_error,omitempty"`
	DBWritableError string `json:"db_writable_error,omitempty"`
}

type SetupApplyRequest struct {
	Config    Config `json:"config"`
	Overwrite bool   `json:"overwrite"`
	InitDB    bool   `json:"init_db"`
	TestIDP   bool   `json:"test_idp"`
}

type SetupApplyResult struct {
	ConfigPath string         `json:"config_path"`
	DBPath     string         `json:"db_path"`
	IDPTest    *IDPTestResult `json:"idp_test,omitempty"`
}

func GetSetupStatus(configPath, dbPath string) SetupStatus {
	status := SetupStatus{
		ConfigPath: configPath,
		DBPath:     dbPath,
	}

	if _, err := os.Stat(configPath); err == nil {
		status.ConfigExists = true
		cfg, err := LoadConfig(configPath)
		if err != nil {
			status.ConfigError = err.Error()
		} else {
			status.ConfigValid = true
			if status.DBPath == "" {
				status.DBPath = cfg.SQLite.Path
			}
		}
	} else if !os.IsNotExist(err) {
		status.ConfigError = err.Error()
	}
	if err := checkConfigWritable(configPath); err != nil {
		status.ConfigWritableError = err.Error()
	} else {
		status.ConfigWritable = true
	}

	if status.DBPath == "" {
		status.DBError = "sqlite path is empty"
		return status
	}
	dbStatus, err := inspectDBStatus(status.DBPath)
	if err != nil {
		status.DBError = err.Error()
	}
	status.DBExists = dbStatus.exists
	status.DBInitialized = dbStatus.initialized
	status.DBWritable = dbStatus.writable
	status.DBWritableError = dbStatus.writableError
	return status
}

func ApplySetup(ctx context.Context, configPath, defaultDBPath string, req SetupApplyRequest) (*SetupApplyResult, error) {
	cfg := req.Config
	if cfg.SQLite.Path == "" {
		cfg.SQLite.Path = defaultDBPath
	}
	if err := ValidateConfig(&cfg); err != nil {
		return nil, err
	}

	var idpResult *IDPTestResult
	if req.TestIDP {
		result, err := TestIdentityProvider(ctx, &cfg)
		if err != nil {
			return nil, err
		}
		idpResult = result
	}

	if req.InitDB {
		db, err := InitMailcloakDB(cfg.SQLite.Path)
		if err != nil {
			return nil, err
		}
		if err := db.Close(); err != nil {
			return nil, err
		}
	}

	if err := writeConfigFile(configPath, &cfg, req.Overwrite); err != nil {
		return nil, err
	}

	return &SetupApplyResult{
		ConfigPath: configPath,
		DBPath:     cfg.SQLite.Path,
		IDPTest:    idpResult,
	}, nil
}

func writeConfigFile(path string, cfg *Config, overwrite bool) error {
	if path == "" {
		return fmt.Errorf("config path is empty")
	}
	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create config directory %s: %w", dir, err)
		}
	}
	flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if !overwrite {
		flags |= os.O_EXCL
	}
	f, err := os.OpenFile(path, flags, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("config already exists at %s", path)
		}
		return fmt.Errorf("open config file %s: %w", path, err)
	}
	defer f.Close()

	enc := yaml.NewEncoder(f)
	enc.SetIndent(2)
	if err := enc.Encode(cfg); err != nil {
		return fmt.Errorf("write config yaml: %w", err)
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("close config yaml encoder: %w", err)
	}
	return nil
}

type dbStatus struct {
	exists        bool
	initialized   bool
	writable      bool
	writableError string
}

func inspectDBStatus(path string) (dbStatus, error) {
	if path != ":memory:" && !strings.HasPrefix(path, "file:") {
		if _, err := os.Stat(path); err != nil {
			if os.IsNotExist(err) {
				if err := checkCreatablePath(path); err != nil {
					return dbStatus{writableError: err.Error()}, nil
				}
				return dbStatus{writable: true}, nil
			}
			return dbStatus{writableError: err.Error()}, err
		}
	}

	db, err := OpenExistingMailcloakDB(path)
	if err != nil {
		return dbStatus{exists: true}, err
	}
	defer db.Close()

	writable := true
	var writableError string
	if err := checkSQLiteWritable(db.DB); err != nil {
		writable = false
		writableError = err.Error()
	}
	initialized, err := schemaInitialized(db.DB)
	if err != nil {
		return dbStatus{exists: true, writable: writable, writableError: writableError}, err
	}
	return dbStatus{exists: true, initialized: initialized, writable: writable, writableError: writableError}, nil
}

func schemaInitialized(db *sql.DB) (bool, error) {
	requiredTables := []string{"domains", "aliases", "apps", "app_from"}
	for _, table := range requiredTables {
		var name string
		err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name)
		if err == sql.ErrNoRows {
			return false, nil
		}
		if err != nil {
			return false, err
		}
	}
	return true, nil
}

func checkConfigWritable(path string) error {
	if path == "" {
		return fmt.Errorf("config path is empty")
	}
	return checkFileOrCreatablePath(path)
}

func checkFileOrCreatablePath(path string) error {
	info, err := os.Stat(path)
	if err == nil {
		if info.IsDir() {
			return fmt.Errorf("%s is a directory", path)
		}
		f, err := os.OpenFile(path, os.O_WRONLY, 0)
		if err != nil {
			return err
		}
		return f.Close()
	}
	if !os.IsNotExist(err) {
		return err
	}
	return checkCreatablePath(path)
}

func checkCreatablePath(path string) error {
	dir, err := nearestExistingDir(filepath.Dir(path))
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".mailcloak-write-test-*")
	if err != nil {
		return err
	}
	name := f.Name()
	closeErr := f.Close()
	removeErr := os.Remove(name)
	if closeErr != nil {
		return closeErr
	}
	if removeErr != nil {
		return removeErr
	}
	return nil
}

func nearestExistingDir(dir string) (string, error) {
	if dir == "" {
		dir = "."
	}
	for {
		info, err := os.Stat(dir)
		if err == nil {
			if !info.IsDir() {
				return "", fmt.Errorf("%s is not a directory", dir)
			}
			return dir, nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", err
		}
		dir = parent
	}
}

func checkSQLiteWritable(db *sql.DB) error {
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return err
	}
	_, err = conn.ExecContext(ctx, `ROLLBACK`)
	return err
}
