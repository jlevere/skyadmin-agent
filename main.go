package main

import (
	"encoding/json"
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

// parseCaptivePortalURL extracts query parameters from a captive portal URL.
func parseCaptivePortalURL(urlStr string) (map[string]string, error) {
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return nil, fmt.Errorf("parsing URL %q: %w", urlStr, err)
	}

	queryParams := parsedURL.Query()
	data := make(map[string]string)
	keys := []string{"UI", "NI", "UIP", "MA", "RN", "PORT", "RAD", "PP", "PMS", "SIP", "OS"}
	for _, key := range keys {
		if values, ok := queryParams[key]; ok && len(values) > 0 {
			data[key] = values[0]
		}
	}

	return data, nil
}

// checkDeviceStatus checks if the device is online or behind a captive portal.
// It only attempts to parse the captive portal URL if its domain is "splash.skyadmin.io".
func checkDeviceStatus() (map[string]string, error) {
	const checkURL = "http://detectportal.firefox.com/success.txt?ipv4"
	client := resty.New().SetTimeout(10 * time.Second)

	slog.Debug("Checking device status", "url", checkURL)
	resp, err := client.R().Get(checkURL)
	if err != nil {
		slog.Error("Failed to check device status", "error", err)
		return nil, fmt.Errorf("device status check failed: %w", err)
	}

	// If we get the expected success response, the device is online.
	if resp.StatusCode() == http.StatusOK && string(resp.Body()) == "success\n" {
		slog.Info("Device is online and responding correctly")
		return nil, nil
	}

	captiveURL, err := url.Parse(resp.Request.URL)
	if err != nil {
		slog.Error("Failed to parse captive portal URL", "error", err)
		return nil, fmt.Errorf("failed to parse captive portal URL: %w", err)
	}

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

	// Extract API token from response body
	re := regexp.MustCompile(`E="([A-Za-z0-9]{32})"`)
	matches := re.FindStringSubmatch(string(resp.Body()))
	if len(matches) >= 2 {
		parsedData["api_token"] = matches[1]
		slog.Debug("Found API token in captive portal response")
	} else {
		slog.Warn("No API token found in captive portal response body")
	}

	return parsedData, nil
}

// checkPortalRegistration attempts to check if the device is already registered.
func checkPortalRegistration(apiToken, vlan, macAddress, ipAddress, nseid string) (*PortalResponse, error) {
	const apiURL = "/api/portals"
	client := resty.New()

	headers := map[string]string{
		"api-token": apiToken,
	}
	payload := map[string]interface{}{
		"vlan":        vlan,
		"mac_address": macAddress,
		"ip_address":  ipAddress,
		"nseid":       nseid,
	}

	slog.Debug("Attempting to login", "url", apiURL, "payload", payload)
	resp, err := client.R().SetHeaders(headers).SetBody(payload).Post(apiURL)
	if err != nil {
		return nil, fmt.Errorf("failed to login: %w", err)
	}

	var portalResp PortalResponse
	if err := json.Unmarshal(resp.Body(), &portalResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal portal response: %w", err)
	}

	return &portalResp, nil
}

// checkPinRequired checks if a PIN is required by the backend.  This also serves to
// validate that the user exists in the system.
func checkPinRequired(apiToken, lastName, roomNumber string, property_id int) (bool, error) {
	const apiURL = "https://skyadmin.io/api/portals"
	client := resty.New()

	headers := map[string]string{
		"api-token": apiToken,
	}
	payload := map[string]interface{}{
		"property_id": property_id,
		"lastname":    lastName,
		"roomnumber":  roomNumber,
	}

	slog.Debug("Checking if PIN is required", "url", apiURL, "payload", payload)
	resp, err := client.R().SetHeaders(headers).SetBody(payload).Post(apiURL)
	if err != nil {
		return false, fmt.Errorf("failed to check PIN status: %w", err)
	}

	var pinResp PinRequiredResponse
	if err := json.Unmarshal(resp.Body(), &pinResp); err != nil {
		return false, fmt.Errorf("failed to unmarshal PIN response: %w", err)
	}

	return pinResp.Data.PinRequired, nil
}

func registerUser(apiToken, lastName, roomNumber, macAddress, ipAddress, nseid string, propertyID, vlanID, registrationMethodID, ratePlanID int) (*RegistrationResponse, error) {
	const apiURL = "https://skyadmin.io/api/portalregistrations"
	client := resty.New()

	headers := map[string]string{
		"api-token": apiToken,
	}
	payload := map[string]interface{}{
		"nseid":                  nseid,
		"property_id":            propertyID,
		"vlan_id":                vlanID,
		"mac_address":            macAddress,
		"ip_address":             ipAddress,
		"registration_method_id": registrationMethodID,
		"rateplan_id":            ratePlanID,
		"last_name":              lastName,
		"room_number":            roomNumber,
	}

	slog.Debug("Attempting registration", "url", apiURL, "payload", payload)
	resp, err := client.R().SetHeaders(headers).SetBody(payload).Post(apiURL)
	if err != nil {
		return nil, fmt.Errorf("failed to register: %w", err)
	}

	var registrationResp RegistrationResponse
	if err := json.Unmarshal(resp.Body(), &registrationResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal registration response: %w", err)
	}

	return &registrationResp, nil
}

func main() {
	debug := flag.Bool("debug", false, "Enable debug logging")
	flag.Parse()

	logLevel := os.Getenv("LOG_LEVEL")
	var level slog.Level
	if logLevel == "debug" || *debug {
		level = slog.LevelDebug
	} else {
		level = slog.LevelInfo
	}

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level})))
	slog.Info("Starting device monitor", "log_level", level.String())

	envars := loadEnvVars()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		slog.Info("Checking device status...")
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

		// Prefer extracted API token over environment variable
		apiToken := envars.APIToken
		if dynamicToken, ok := portalData["api_token"]; ok && dynamicToken != "" {
			apiToken = dynamicToken
		}

		slog.Info("Captive portal detected; attempting registration flow", "portalData", portalData)

		vlan := portalData["PORT"]
		macAddress := portalData["MA"]
		ipAddress := portalData["SIP"]
		nseid := portalData["UI"]

		portalResp, err := checkPortalRegistration(apiToken, vlan, macAddress, ipAddress, nseid)
		if err != nil {
			slog.Error("Portal registration check failed", "error", err)
			continue
		}

		propertyID := portalResp.Data.PropertyID
		vlanID := portalResp.Data.VlanID

		if portalResp.Data.RegistrationStatus == "Successful" {
			slog.Info("Device is already authenticated; registration complete")
			continue
		}

		pinRequired, err := checkPinRequired(apiToken, envars.LastName, envars.RoomNumber, propertyID)
		if err != nil {
			slog.Error("Failed to check if PIN is required", "error", err)
			continue
		}

		if !pinRequired {
			slog.Error("User lookup failed or PIN not required")
			continue
		}

		regResp, err := registerUser(
			apiToken,
			envars.LastName,
			envars.RoomNumber,
			macAddress,
			ipAddress,
			nseid,
			propertyID,
			vlanID,
			envars.RegistrationMethodID,
			envars.RatePlanID,
		)
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
