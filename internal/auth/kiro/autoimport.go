package kiro

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// AutoImportResult is a normalized Kiro credential discovered on disk.
type AutoImportResult struct {
	Found         bool     `json:"found"`
	Source        string   `json:"source,omitempty"`
	TriedPaths    []string `json:"tried_paths,omitempty"`
	AccessToken   string   `json:"access_token,omitempty"`
	RefreshToken  string   `json:"refresh_token,omitempty"`
	ExpiresAt     int64    `json:"expires_at,omitempty"`
	Region        string   `json:"region,omitempty"`
	ProfileArn    string   `json:"profile_arn,omitempty"`
	AuthMethod    string   `json:"auth_method,omitempty"`
	ClientID      string   `json:"client_id,omitempty"`
	ClientSecret  string   `json:"client_secret,omitempty"`
	TokenEndpoint string   `json:"token_endpoint,omitempty"`
	Scopes        string   `json:"scopes,omitempty"`
	Error         string   `json:"error,omitempty"`
}

// AutoImport searches local Kiro credentials (kiro-cli SQLite, AWS SSO cache,
// Kiro IDE profile.json) and returns a normalized result. It never writes to
// user files.
func AutoImport(ctx context.Context) (*AutoImportResult, error) {
	return autoImportWithSearchRoots(ctx, defaultSearchRoots())
}

// autoImportWithSearchRoots is the testable core of AutoImport. It accepts
// explicit roots instead of deriving them from the current user/home directory.
func autoImportWithSearchRoots(ctx context.Context, roots searchRoots) (*AutoImportResult, error) {
	res, err := discoverKiroCliSQLite(roots.sqlitePaths)
	if err != nil {
		return nil, err
	}
	if res != nil && res.Found {
		return res, nil
	}

	res, err = discoverAwsSsoCache(roots.awsSsoDir, roots.profilePaths)
	if err != nil {
		return nil, err
	}
	if res != nil && res.Found {
		return res, nil
	}

	tried := append([]string{}, roots.sqlitePaths...)
	if roots.awsSsoDir != "" {
		tried = append(tried, roots.awsSsoDir)
	}
	notFound := "Kiro credentials not found. Run `kiro-cli login --use-device-flow` then retry, or use the Import Token option in the dashboard."
	return &AutoImportResult{Found: false, TriedPaths: tried, Error: notFound}, nil
}

type searchRoots struct {
	sqlitePaths  []string
	awsSsoDir    string
	profilePaths []string
}

func defaultSearchRoots() searchRoots {
	home, _ := os.UserHomeDir()
	appData := os.Getenv("APPDATA")

	var sqlitePaths []string
	if home != "" {
		sqlitePaths = append(sqlitePaths, filepath.Join(home, ".local", "share", "kiro-cli", "data.sqlite3"))
		sqlitePaths = append(sqlitePaths, filepath.Join(home, "Library", "Application Support", "kiro", "storage.db"))
	}
	if appData != "" {
		sqlitePaths = append(sqlitePaths, filepath.Join(appData, "kiro", "storage.db"))
	} else if home != "" {
		sqlitePaths = append(sqlitePaths, filepath.Join(home, "AppData", "Roaming", "kiro", "storage.db"))
	}

	var awsSsoDir string
	if home != "" {
		awsSsoDir = filepath.Join(home, ".aws", "sso", "cache")
	}

	return searchRoots{sqlitePaths: sqlitePaths, awsSsoDir: awsSsoDir, profilePaths: kiroIdeProfilePaths(home, appData)}
}

func kiroIdeProfilePaths(home, appData string) []string {
	var paths []string
	if appData != "" {
		paths = append(paths, filepath.Join(appData, "Kiro", "User", "globalStorage", "kiro.kiroagent", "profile.json"))
	} else if home != "" {
		paths = append(paths, filepath.Join(home, "AppData", "Roaming", "Kiro", "User", "globalStorage", "kiro.kiroagent", "profile.json"))
	}
	if home != "" {
		paths = append(paths, filepath.Join(home, ".config", "Kiro", "User", "globalStorage", "kiro.kiroagent", "profile.json"))
		paths = append(paths, filepath.Join(home, "Library", "Application Support", "Kiro", "User", "globalStorage", "kiro.kiroagent", "profile.json"))
	}
	return paths
}

// ValidateDiscoveredCredential returns an error if a discovered result is not
// usable for import.
func ValidateDiscoveredCredential(res *AutoImportResult) error {
	if res == nil || !res.Found {
		return errors.New("no usable Kiro credentials found")
	}
	if strings.TrimSpace(res.RefreshToken) == "" {
		return errors.New("discovered credential is missing a refresh token")
	}
	if strings.TrimSpace(res.AccessToken) == "" {
		return errors.New("discovered credential is missing an access token")
	}
	if res.AuthMethod == "external_idp" {
		if _, err := validateExternalIdpTokenEndpoint(res.TokenEndpoint); err != nil {
			return err
		}
	}
	return nil
}

// --- SQLite (kiro-cli) ------------------------------------------------------

var sqliteTokenKeys = []string{
	"kirocli:odic:token",
	"kirocli:oidc:token",
	"kiro:auth:token",
}

var sqliteRegistrationKeys = []string{
	"kirocli:odic:device-registration",
	"kirocli:oidc:device-registration",
}

var sqliteTables = []string{"auth_kv", "ItemTable", "storage"}

func discoverKiroCliSQLite(paths []string) (*AutoImportResult, error) {
	var tried []string
	for _, p := range paths {
		tried = append(tried, p)
		db, err := openSQLiteReadOnly(p)
		if err != nil {
			continue
		}
		res, err := readKiroSQLite(db)
		db.Close()
		if err != nil {
			return nil, err
		}
		if res != nil {
			res.Source = "kiro-cli-sqlite"
			res.TriedPaths = tried
			return res, nil
		}
	}
	return nil, nil
}

func openSQLiteReadOnly(path string) (*sql.DB, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	dsn := path + "?_pragma=query_only(1)&_pragma=journal_mode(OFF)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func readKiroSQLite(db *sql.DB) (*AutoImportResult, error) {
	tokenData, err := queryFirstJSON(db, sqliteTables, sqliteTokenKeys)
	if err != nil {
		return nil, err
	}
	if tokenData == nil {
		return nil, nil
	}
	refreshToken := stringField(tokenData, "refresh_token", "refreshToken")
	if refreshToken == "" {
		return nil, nil
	}

	regData, _ := queryFirstJSON(db, sqliteTables, sqliteRegistrationKeys)

	profileArn := readProfileArnFromSQLite(db)
	if profileArn == "" {
		profileArn = readKiroIdeProfileArn(defaultSearchRoots().profilePaths)
	}

	region := stringField(tokenData, "region")
	if region == "" {
		region = stringField(regData, "region")
	}
	if region == "" && profileArn != "" {
		region = regionFromProfileArn(profileArn)
	}
	if region == "" {
		region = defaultRegion
	}

	expiresAt := parseExpiresAt(stringField(tokenData, "expires_at", "expiresAt", "expiresAtUTC"))

	res := &AutoImportResult{
		Found:        true,
		AccessToken:  stringField(tokenData, "access_token", "accessToken"),
		RefreshToken: refreshToken,
		ExpiresAt:    expiresAt,
		Region:       region,
		ProfileArn:   profileArn,
		ClientID:     stringField(regData, "client_id", "clientId"),
		ClientSecret: stringField(regData, "client_secret", "clientSecret"),
	}
	res.AuthMethod = authMethodFromRefreshToken(res.RefreshToken)
	return res, nil
}

func readProfileArnFromSQLite(db *sql.DB) string {
	const profileKey = "api.codewhisperer.profile"
	row, err := queryFirstJSON(db, sqliteTables, []string{profileKey})
	if err != nil || row == nil {
		return ""
	}
	return stringField(row, "arn", "profileArn")
}

func queryFirstJSON(db *sql.DB, tables, keys []string) (map[string]any, error) {
	for _, key := range keys {
		for _, table := range tables {
			var value string
			err := db.QueryRow(fmt.Sprintf("SELECT value FROM %s WHERE key = ?", table), key).Scan(&value)
			if err != nil {
				continue
			}
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			var obj map[string]any
			if json.Unmarshal([]byte(value), &obj) != nil {
				continue
			}
			return obj, nil
		}
	}
	return nil, nil
}


// --- AWS SSO cache JSON fallback --------------------------------------------

func discoverAwsSsoCache(dir string, profilePaths []string) (*AutoImportResult, error) {
	if dir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil
	}

	preferred := []string{"kiro-auth-token.json", "amazon-q-auth-token.json"}
	ordered := make([]string, 0, len(entries))
	for _, name := range preferred {
		if hasFile(entries, name) {
			ordered = append(ordered, name)
		}
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		found := false
		for _, p := range preferred {
			if p == name {
				found = true
				break
			}
		}
		if !found {
			ordered = append(ordered, name)
		}
	}

	for _, name := range ordered {
		path := filepath.Join(dir, name)
		body, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var data map[string]any
		if err := json.Unmarshal(body, &data); err != nil {
			continue
		}

		refreshToken := stringField(data, "refreshToken", "refresh_token")
		if refreshToken == "" {
			continue
		}

		authMethod := strings.ToLower(stringField(data, "authMethod", "auth_method"))
		provider := strings.ToLower(stringField(data, "provider"))
		isExternalIdp := isExternalIdpAuthMethod(authMethod) || provider == "externalidp"

		if isExternalIdp {
			profileArn := readKiroIdeProfileArn(profilePaths)
			region := stringField(data, "region")
			if region == "" && profileArn != "" {
				region = regionFromProfileArn(profileArn)
			}
			if region == "" {
				region = defaultRegion
			}
			return &AutoImportResult{
				Found:         true,
				Source:        name,
				AccessToken:   stringField(data, "accessToken", "access_token", "accessToken"),
				RefreshToken:  refreshToken,
				ExpiresAt:     parseExpiresAt(stringField(data, "expiresAt", "expires_at")),
				Region:        region,
				ProfileArn:    profileArn,
				AuthMethod:    "external_idp",
				ClientID:      stringField(data, "clientId", "client_id"),
				TokenEndpoint: stringField(data, "tokenEndpoint", "token_endpoint"),
				Scopes:        normalizeScope(stringField(data, "scopes", "scope")),
			}, nil
		}

		if strings.HasPrefix(refreshToken, "aorAAAAAG") {
			region := stringField(data, "region")
			method := authMethod
			if method == "" {
				method = "import"
			}

			clientID, clientSecret := readClientRegistration(dir, stringField(data, "clientIdHash", "client_id_hash", "clientId"))
			if clientID == "" {
				clientID = stringField(data, "clientId", "client_id")
			}

			profileArn := readKiroIdeProfileArn(profilePaths)
			if region == "" && profileArn != "" {
				region = regionFromProfileArn(profileArn)
			}
			if region == "" {
				region = defaultRegion
			}

			return &AutoImportResult{
				Found:        true,
				Source:       name,
				AccessToken:  stringField(data, "accessToken", "access_token"),
				RefreshToken: refreshToken,
				ExpiresAt:    parseExpiresAt(stringField(data, "expiresAt", "expires_at")),
				Region:       region,
				ProfileArn:   profileArn,
				AuthMethod:   method,
				ClientID:     clientID,
				ClientSecret: clientSecret,
			}, nil
		}
	}

	return nil, nil
}

func readClientRegistration(dir, hash string) (string, string) {
	if dir == "" || hash == "" {
		return "", ""
	}
	path := filepath.Join(dir, hash+".json")
	body, err := os.ReadFile(path)
	if err != nil {
		return "", ""
	}
	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		return "", ""
	}
	return stringField(data, "clientId", "client_id"), stringField(data, "clientSecret", "client_secret")
}

func readKiroIdeProfileArn(paths []string) string {
	for _, p := range paths {
		body, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var data map[string]any
		if err := json.Unmarshal(body, &data); err != nil {
			continue
		}
		if arn := stringField(data, "arn", "profileArn"); arn != "" {
			return arn
		}
	}
	return ""
}

// --- helpers ----------------------------------------------------------------

func hasFile(entries []os.DirEntry, name string) bool {
	for _, e := range entries {
		if e.Name() == name {
			return true
		}
	}
	return false
}

func stringField(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if m == nil {
			return ""
		}
		v, ok := m[k]
		if !ok {
			continue
		}
		switch s := v.(type) {
		case string:
			return strings.TrimSpace(s)
		case []byte:
			return strings.TrimSpace(string(s))
		default:
			return strings.TrimSpace(fmt.Sprintf("%v", s))
		}
	}
	return ""
}

func parseExpiresAt(raw string) int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Now().Add(time.Hour).Unix()
	}
	if layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02T15:04:05Z",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}; true {
		for _, layout := range layouts {
			if t, err := time.Parse(layout, raw); err == nil {
				return t.Unix()
			}
		}
	}
	if ms, err := strconv.ParseFloat(raw, 64); err == nil {
		if ms > 1e12 {
			return int64(ms / 1000)
		}
		if ms > 1e9 {
			return int64(ms)
		}
	}
	if sec, err := strconv.ParseInt(raw, 10, 64); err == nil {
		if sec > 1e12 {
			return sec / 1000
		}
		return sec
	}
	return time.Now().Add(time.Hour).Unix()
}

func regionFromProfileArn(arn string) string {
	parts := strings.Split(arn, ":")
	if len(parts) >= 4 {
		return parts[3]
	}
	return ""
}

func isExternalIdpAuthMethod(method string) bool {
	return strings.ToLower(strings.TrimSpace(method)) == "external_idp"
}

func authMethodFromRefreshToken(token string) string {
	_ = token
	return "import"
}

// AutoImport exposes credential discovery to the Kiro auth handler.
func (s *KiroAuthService) AutoImport(ctx context.Context) (*AutoImportResult, error) {
	return AutoImport(ctx)
}

