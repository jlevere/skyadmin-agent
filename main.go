package main

import (
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"time"

	"github.com/go-resty/resty/v2"
)

// version can be set at build time using ldflags
// Example: go build -ldflags "-X main.version=v1.0.0"
var version = "dev"

type RegistrationResponse struct {
	Status string `json:"registration_status"`
	URL    string `json:"url"`
	Error  string `json:"error"`
}

type PinRequiredResponse struct {
	Data struct {
		PinRequired bool `json:"pin_required"`
	} `json:"data"`
}

type PortalResponse struct {
	Data struct {
		RegistrationStatus string `json:"registration_status,omitempty"`
		VlanID             int    `json:"vlan_id"`
		PropertyID         int    `json:"property_id"`
	} `json:"data"`
}

type RegistrationData struct {
	VLAN                 string `json:"vlan"`
	MacAddress           string `json:"mac_address"`
	IpAddress            string `json:"ip_address"`
	Nseid                string `json:"nseid"`
	LastName             string `json:"lastname"`
	RoomNumber           string `json:"roomnumber"`
	PropertyID           int    `json:"property_id"`
	RegistrationMethodID int    `json:"registration_method_id"`
	RatePlanID           int    `json:"rateplan_id"`
	Pin                  string `json:"pin"`
}

type EnvVars struct {
	APIToken             string
	Vlan                 string
	MacAddress           string
	IPAddress            string
	Nseid                string
	LastName             string
	RoomNumber           string
	PropertyID           int
	RegistrationMethodID int
	RatePlanID           int
}

type APIClient struct {
	client   *resty.Client
	env      EnvVars
	apiToken string
}

const (
	BaseURL          = "https://skyadmin.io/api"
	DefaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:135.0) Gecko/20100101 Firefox/135.0"
	RequestTimeout   = time.Second * 10
	RetryCount       = 10
	RetryWaitTime    = time.Second * 10
)

func NewAPIClient(env EnvVars) *APIClient {
	client := resty.New().
		SetBaseURL(BaseURL).
		SetHeader("User-Agent", DefaultUserAgent).
		SetHeader("Accept", "application/json").
		SetHeader("Content-Type", "application/json").
		SetTimeout(RequestTimeout).
		SetRetryCount(RetryCount).
		SetRetryWaitTime(RetryWaitTime).
		OnBeforeRequest(func(c *resty.Client, r *resty.Request) error {
			slog.Debug("Request started", "method", r.Method, "url", r.URL)
			return nil
		}).
		OnAfterResponse(func(c *resty.Client, r *resty.Response) error {
			if r.IsError() {
				slog.Error("Request failed", "status", r.Status(), "body", string(r.Body()))
			}
			return nil
		})

	return &APIClient{
		client:   client,
		env:      env,
		apiToken: env.APIToken,
	}
}

// Generic getEnv function to retrieve environment variables
func getEnv[T int | string](key string, fallback T) T {
	valueStr, ok := os.LookupEnv(key)
	if !ok {
		slog.Warn(fmt.Sprintf("%s is not set, using default %v", key, fallback))
		return fallback
	}

	var value T
	switch any(fallback).(type) {
	case int:
		parsed, err := strconv.Atoi(valueStr)
		if err != nil {
			slog.Warn(fmt.Sprintf("failed to parse %s as int, using default %v", key, fallback))
			return fallback
		}
		value = any(parsed).(T)
	case string:
		value = any(valueStr).(T)
	default:
		return fallback
	}

	return value
}

func loadEnvVars() EnvVars {
	return EnvVars{
		APIToken:             getEnv("API_TOKEN", "b2507058a2c145d60c6d919c0347fe9c"),
		Vlan:                 getEnv("VLAN", "3300"),
		MacAddress:           getEnv("MAC_ADDRESS", "D4CA6DA65E0E"),
		IPAddress:            getEnv("IP_ADDRESS", "10.0.24.21"),
		Nseid:                getEnv("NSEID", "a39d49"),
		LastName:             getEnv("LASTNAME", "Michael"),
		RoomNumber:           getEnv("ROOMNUMBER", "101"),
		PropertyID:           getEnv("PROPERTYID", 1234),
		RegistrationMethodID: getEnv("REGMETHODID", 2),
		RatePlanID:           getEnv("RATEPLANID", 3),
	}
}

// checkDeviceStatus checks if the device is online or behind a captive portal.
// It only attempts to parse the captive portal URL if its domain is "splash.skyadmin.io".
func checkDeviceStatus() (map[string]string, error) {
	const checkURL = "http://detectportal.firefox.com/success.txt?ipv4"
	client := resty.New().
		SetTimeout(30*time.Second).
		SetBaseURL(checkURL).
		SetHeader("User-Agent", DefaultUserAgent)

	resp, err := client.R().Get("/success.txt?ipv4")
	if err != nil {
		slog.Error("Failed to check device status", "error", err)
		return nil, fmt.Errorf("device status check failed: %w", err)
	}

	// If we get the expected success response, the device is online.
	if resp.StatusCode() == http.StatusOK && string(resp.Body()) == "success\n" {
		slog.Debug("Device is online and responding correctly")
		return nil, nil
	}

	captiveURL := resp.RawResponse.Request.URL

	// Only proceed if the URL's hostname is "splash.skyadmin.io".
	if captiveURL.Hostname() != "splash.skyadmin.io" {
		slog.Warn("Unexpected captive portal domain", "host", captiveURL.Hostname())
		return nil, fmt.Errorf("unexpected domain: %s", captiveURL.Hostname())
	}

	slog.Warn("Captive portal detected", "status", resp.StatusCode(), "url", captiveURL.String())

	parsedData, err := parseCaptivePortalURL(captiveURL.String())
	if err != nil {
		slog.Error("Failed to parse captive portal URL", "error", err)
		return nil, fmt.Errorf("failed to parse captive portal URL: %w", err)
	}
	return parsedData, nil
}

// regex the webpack to find the api key to use for subsequent reqests.
// The path to use for this webpack js comes from the body of the splash page
func (a *APIClient) GetAPIToken(path string) (string, error) {

	resp, err := a.client.R().Get("/js/app.e360d181.js") //TODO: dont hardcode this
	if err != nil {
		return "", fmt.Errorf("portal check failed: %w", err)
	}

	if resp.IsError() {
		return "", fmt.Errorf("portal check failed with status %d", resp.StatusCode())
	}

	api_token := extractAPIToken(string(resp.Body()))
	if len(api_token) != 0 {
		slog.Debug("Found API token in captive portal response", "api_token", api_token)
	}

	return api_token, nil
}

// checkPortalRegistration attempts to check if the device is already registered.
func (a *APIClient) CheckPortalRegistration(portalData map[string]string) (*PortalResponse, error) {
	req := a.client.R().
		SetHeader("api-token", a.apiToken).
		SetBody(map[string]interface{}{
			"vlan":        portalData["PORT"],
			"mac_address": portalData["MA"],
			"ip_address":  portalData["SIP"],
			"nseid":       portalData["UI"],
		})

	var response PortalResponse
	resp, err := req.SetResult(&response).Post("/portals")
	if err != nil {
		return nil, fmt.Errorf("portal check failed: %w", err)
	}

	if resp.IsError() {
		return nil, fmt.Errorf("portal check failed with status %d", resp.StatusCode())
	}

	return &response, nil
}

// checkPinRequired checks if a PIN is required by the backend.  This also serves to
// validate that the user exists in the system.
func (a *APIClient) CheckPinRequired(propertyID int) (bool, error) {
	req := a.client.R().
		SetHeader("api-token", a.apiToken).
		SetBody(map[string]interface{}{
			"property_id": propertyID,
			"lastname":    a.env.LastName,
			"roomnumber":  a.env.RoomNumber,
		})

	var response PinRequiredResponse
	resp, err := req.SetResult(&response).Post("/skypms/pinrequired")
	if err != nil {
		return false, fmt.Errorf("pin check failed: %w", err)
	}

	if resp.IsError() {
		return false, fmt.Errorf("pin check failed with status %d", resp.StatusCode())
	}

	return response.Data.PinRequired, nil
}

func (a *APIClient) RegisterDevice(portalData map[string]string, propertyID, vlanID int) (*RegistrationResponse, error) {

	req := a.client.R().
		SetHeader("api-token", a.apiToken).
		SetBody(map[string]interface{}{
			"nseid":                  portalData["UI"],
			"property_id":            propertyID,
			"vlan_id":                vlanID,
			"mac_address":            portalData["MA"],
			"ip_address":             portalData["SIP"],
			"registration_method_id": a.env.RegistrationMethodID,
			"rateplan_id":            a.env.RatePlanID,
			"last_name":              a.env.LastName,
			"room_number":            a.env.RoomNumber,
		})

	var response RegistrationResponse
	resp, err := req.SetResult(&response).Post("/portalregistrations")
	if err != nil {
		return nil, fmt.Errorf("registration failed: %w", err)
	}

	if resp.IsError() {
		return nil, fmt.Errorf("registration failed with status %d", resp.StatusCode())
	}

	return &response, nil
}

// Parse the url of a captive portal session. Extracts url paramiters
func parseCaptivePortalURL(urlStr string) (map[string]string, error) {
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return nil, fmt.Errorf("parsing URL %q: %w", urlStr, err)
	}

	data := make(map[string]string)
	query := parsedURL.Query()
	for _, key := range []string{"UI", "NI", "UIP", "MA", "RN", "PORT", "RAD", "PP", "PMS", "SIP", "OS"} {
		if val := query.Get(key); val != "" {
			data[key] = val
		}
	}
	return data, nil
}

func extractAPIToken(body string) string {
	re := regexp.MustCompile(`E="([A-Za-z0-9]{32})"`)
	if matches := re.FindStringSubmatch(body); len(matches) >= 2 {
		return matches[1]
	}
	return ""
}

func configureLogging(debug bool) {

	logLevel := os.Getenv("LOG_LEVEL")
	var level slog.Level
	if logLevel == "debug" || debug {
		level = slog.LevelDebug
	} else {
		level = slog.LevelInfo
	}

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level})))
	slog.Info("Starting skyadmin-agent", "version", version, "log_level", level.String())
}

func main() {
	debug := flag.Bool("debug", false, "Enable debug logging")
	showVersion := flag.Bool("version", false, "Show version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("skyadmin-agent version %s\n", version)
		os.Exit(0)
	}

	configureLogging(*debug)

	env := loadEnvVars()

	client := NewAPIClient(env)

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		slog.Debug("Checking device status...")
		portalData, err := checkDeviceStatus()
		if err != nil {
			slog.Warn("Device check failed, will retry on next cycle", "error", err)
			continue
		}

		// If no portalData is returned then the device is online.
		if portalData == nil {
			slog.Debug("Device online; no captive portal detected.")
			continue
		}

		api_token, err := client.GetAPIToken("")
		if err != nil {
			slog.Error("Failed to get updated api token", "error", err)
		}

		// Prefer extracted API token over environment variable
		if api_token != "" {
			env.APIToken = api_token
		}

		slog.Info("Captive portal detected; attempting registration flow", "portalData", portalData)

		portalResp, err := client.CheckPortalRegistration(portalData)
		if err != nil {
			slog.Error("Portal registration check failed", "error", err)
			continue
		}

		if portalResp.Data.RegistrationStatus == "Successful" {
			slog.Info("Device is already authenticated; registration complete")
			continue
		}

		pinRequired, err := client.CheckPinRequired(portalResp.Data.PropertyID)
		if err != nil {
			slog.Error("Failed to check if PIN is required", "error", err)
			continue
		}

		if !pinRequired {
			slog.Error("User lookup failed or PIN not required")
			continue
		}

		regResp, err := client.RegisterDevice(portalData, portalResp.Data.PropertyID, portalResp.Data.VlanID)
		if err != nil {
			slog.Error("User registration failed", "error", err)
			continue
		}

		if regResp.Status == "Successful" {
			slog.Info("User registration successful", "url", regResp.URL)
		} else {
			slog.Error("Registration failed", "error", regResp.Error)
		}
	}
}
