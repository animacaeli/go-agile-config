package agileconfig

import "context"

// ServiceStatus is the AgileConfig service health status.
type ServiceStatus int

const (
	ServiceStatusUnhealthy ServiceStatus = 0
	ServiceStatusHealthy   ServiceStatus = 1
)

// ServiceQueryStatus selects which registered services to fetch.
type ServiceQueryStatus int

const (
	ServiceQueryStatusAll ServiceQueryStatus = iota
	ServiceQueryStatusOnline
	ServiceQueryStatusOffline
)

// HeartbeatMode is the AgileConfig service heartbeat mode.
type HeartbeatMode string

const (
	HeartbeatModeClient HeartbeatMode = "client"
	HeartbeatModeServer HeartbeatMode = "server"
	HeartbeatModeNone   HeartbeatMode = "none"
)

// ServiceInfo describes one service instance registered in AgileConfig.
type ServiceInfo struct {
	ServiceID   string        `json:"ServiceId"`
	ServiceName string        `json:"ServiceName"`
	IP          string        `json:"Ip"`
	Port        *int          `json:"Port"`
	MetaData    []string      `json:"MetaData"`
	Status      ServiceStatus `json:"Status"`
}

// RegisterService is the payload used to register a service instance.
type RegisterService struct {
	ServiceID     string        `json:"ServiceId"`
	ServiceName   string        `json:"ServiceName"`
	IP            string        `json:"Ip"`
	Port          *int          `json:"Port"`
	MetaData      []string      `json:"MetaData"`
	CheckURL      string        `json:"CheckUrl,omitempty"`
	AlarmURL      string        `json:"AlarmUrl,omitempty"`
	HeartbeatMode HeartbeatMode `json:"HeartBeatMode,omitempty"`
}

// RegisterResult is returned by AgileConfig after service registration changes.
type RegisterResult struct {
	UniqueID string `json:"UniqueId"`
}

// HeartbeatResult is returned by AgileConfig after a service heartbeat.
type HeartbeatResult struct {
	Module string `json:"Module"`
	Action string `json:"Action"`
	Data   string `json:"Data"`
}

type heartbeatRequest struct {
	UniqueID string `json:"UniqueId"`
}

// ListServices returns all service instances registered in AgileConfig.
func (c *Client) ListServices(ctx context.Context) ([]ServiceInfo, error) {
	return c.tp.listServices(ctx, ServiceQueryStatusAll)
}

// ListOnlineServices returns healthy service instances registered in AgileConfig.
func (c *Client) ListOnlineServices(ctx context.Context) ([]ServiceInfo, error) {
	return c.tp.listServices(ctx, ServiceQueryStatusOnline)
}

// ListOfflineServices returns unhealthy service instances registered in AgileConfig.
func (c *Client) ListOfflineServices(ctx context.Context) ([]ServiceInfo, error) {
	return c.tp.listServices(ctx, ServiceQueryStatusOffline)
}

// RegisterService registers a service instance in AgileConfig.
func (c *Client) RegisterService(ctx context.Context, service RegisterService) (RegisterResult, error) {
	return c.tp.registerService(ctx, service)
}

// UnregisterService removes a service registration by AgileConfig unique ID.
func (c *Client) UnregisterService(ctx context.Context, uniqueID string) (RegisterResult, error) {
	return c.tp.unregisterService(ctx, uniqueID)
}

// Heartbeat reports that a registered service instance is still alive.
func (c *Client) Heartbeat(ctx context.Context, uniqueID string) (HeartbeatResult, error) {
	return c.tp.heartbeat(ctx, uniqueID)
}
