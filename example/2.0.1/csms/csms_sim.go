package main

import (
	"crypto/tls"
	"crypto/x509"
	"io/ioutil"
	"os"
	"strconv"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/lorenzodonini/ocpp-go/ocpp1.6/core"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/availability"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/diagnostics"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/localauth"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/provisioning"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/remotecontrol"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/reservation"
	types2 "github.com/lorenzodonini/ocpp-go/ocpp2.0.1/types"
	"github.com/lorenzodonini/ocpp-go/ocppj"
	"github.com/lorenzodonini/ocpp-go/ws"
)

const (
	defaultListenPort          = 8887
	defaultHeartbeatInterval   = 600
	envVarServerPort           = "SERVER_LISTEN_PORT"
	envVarTls                  = "TLS_ENABLED"
	envVarCaCertificate        = "CA_CERTIFICATE_PATH"
	envVarServerCertificate    = "SERVER_CERTIFICATE_PATH"
	envVarServerCertificateKey = "SERVER_CERTIFICATE_KEY_PATH"
)

var log *logrus.Logger
var csms ocpp2.CSMS

func setupCentralSystem() ocpp2.CSMS {
	return ocpp2.NewCSMS(nil, nil)
}

func setupTlsCentralSystem() ocpp2.CSMS {
	var certPool *x509.CertPool
	// Load CA certificates
	caCertificate, ok := os.LookupEnv(envVarCaCertificate)
	if !ok {
		log.Infof("no %v found, using system CA pool", envVarCaCertificate)
		systemPool, err := x509.SystemCertPool()
		if err != nil {
			log.Fatalf("couldn't get system CA pool: %v", err)
		}
		certPool = systemPool
	} else {
		certPool = x509.NewCertPool()
		data, err := ioutil.ReadFile(caCertificate)
		if err != nil {
			log.Fatalf("couldn't read CA certificate from %v: %v", caCertificate, err)
		}
		ok = certPool.AppendCertsFromPEM(data)
		if !ok {
			log.Fatalf("couldn't read CA certificate from %v", caCertificate)
		}
	}
	certificate, ok := os.LookupEnv(envVarServerCertificate)
	if !ok {
		log.Fatalf("no required %v found", envVarServerCertificate)
	}
	key, ok := os.LookupEnv(envVarServerCertificateKey)
	if !ok {
		log.Fatalf("no required %v found", envVarServerCertificateKey)
	}
	server := ws.NewTLSServer(certificate, key, &tls.Config{
		ClientAuth: tls.RequireAndVerifyClientCert,
		ClientCAs:  certPool,
	})
	return ocpp2.NewCSMS(nil, server)
}

// Run for every connected Charge Point, to simulate some functionality
func exampleRoutine(chargePointID string, handler *CSMSHandler) {
	// Wait for some time
	time.Sleep(2 * time.Second)
	// Reserve a connector
	reservationID := 42
	clientIDTokenType := types2.IdTokenTypeKeyCode
	clientIdTag := "l33t"
	connectorID := 1
	expiryDate := types2.NewDateTime(time.Now().Add(1 * time.Hour))
	cb1 := func(confirmation *reservation.ReserveNowResponse, err error) {
		if err != nil {
			logDefault(chargePointID, reservation.ReserveNowFeatureName).Errorf("error on request: %v", err)
		} else if confirmation.Status == reservation.ReserveNowStatusAccepted {
			logDefault(chargePointID, confirmation.GetFeatureName()).Infof("connector %v reserved for client %v until %v (reservation ID %d)", connectorID, clientIdTag, expiryDate.FormatTimestamp(), reservationID)
		} else {
			logDefault(chargePointID, confirmation.GetFeatureName()).Infof("couldn't reserve connector %v: %v", connectorID, confirmation.Status)
		}
	}
	e := csms.ReserveNow(chargePointID, cb1, reservationID, expiryDate, clientIDTokenType)
	if e != nil {
		logDefault(chargePointID, reservation.ReserveNowFeatureName).Errorf("couldn't send message: %v", e)
		return
	}
	// Wait for some time
	time.Sleep(1 * time.Second)
	// Cancel the reservation
	cb2 := func(confirmation *reservation.CancelReservationResponse, err error) {
		if err != nil {
			logDefault(chargePointID, reservation.CancelReservationFeatureName).Errorf("error on request: %v", err)
		} else if confirmation.Status == reservation.CancelReservationStatusAccepted {
			logDefault(chargePointID, confirmation.GetFeatureName()).Infof("reservation %v canceled successfully", reservationID)
		} else {
			logDefault(chargePointID, confirmation.GetFeatureName()).Infof("couldn't cancel reservation %v", reservationID)
		}
	}
	e = csms.CancelReservation(chargePointID, cb2, reservationID)
	if e != nil {
		logDefault(chargePointID, reservation.ReserveNowFeatureName).Errorf("couldn't send message: %v", e)
		return
	}
	// Wait for some time
	time.Sleep(5 * time.Second)
	// Get current local list version
	cb3 := func(confirmation *localauth.GetLocalListVersionResponse, err error) {
		if err != nil {
			logDefault(chargePointID, localauth.GetLocalListVersionFeatureName).Errorf("error on request: %v", err)
		} else {
			logDefault(chargePointID, confirmation.GetFeatureName()).Infof("current local list version: %v", confirmation.VersionNumber)
		}
	}
	e = csms.GetLocalListVersion(chargePointID, cb3)
	if e != nil {
		logDefault(chargePointID, localauth.GetLocalListVersionFeatureName).Errorf("couldn't send message: %v", e)
		return
	}
	// Wait for some time
	time.Sleep(5 * time.Second)
	setVariableData := []provisioning.SetVariableData{
		{
			AttributeType:  types2.AttributeTarget,
			AttributeValue: "10",
			Component:      types2.Component{Name: "OCPPCommCtrlr"},
			Variable:       types2.Variable{Name: "HeartbeatInterval"},
		},
		{
			AttributeType:  types2.AttributeTarget,
			AttributeValue: "true",
			Component:      types2.Component{Name: "AuthCtrlr"},
			Variable:       types2.Variable{Name: "Enabled"},
		},
	}
	// Change meter sampling values time
	cb4 := func(confirmation *provisioning.SetVariablesResponse, err error) {
		if err != nil {
			logDefault(chargePointID, provisioning.SetVariablesFeatureName).Errorf("error on request: %v", err)
		}
		for _, r := range confirmation.SetVariableResult {
			if r.AttributeStatus == provisioning.SetVariableStatusNotSupported {
				logDefault(chargePointID, confirmation.GetFeatureName()).Warnf("couldn't update variable %v for component %v: unsupported", r.Variable.Name, r.Component.Name)
			} else if r.AttributeStatus == provisioning.SetVariableStatusUnknownComponent {
				logDefault(chargePointID, confirmation.GetFeatureName()).Warnf("couldn't update variable for unknown component %v", r.Component.Name)
			} else if r.AttributeStatus == provisioning.SetVariableStatusUnknownVariable {
				logDefault(chargePointID, confirmation.GetFeatureName()).Warnf("couldn't update unknown variable %v for component %v", r.Variable.Name, r.Component.Name)
			} else if r.AttributeStatus == provisioning.SetVariableStatusRejected {
				logDefault(chargePointID, confirmation.GetFeatureName()).Warnf("couldn't update variable %v for key: %v", r.Variable.Name, r.Component.Name)
			} else {
				logDefault(chargePointID, confirmation.GetFeatureName()).Infof("updated variable %v for component %v", r.Variable.Name, r.Component.Name)
			}
		}
	}
	e = csms.SetVariables(chargePointID, cb4, setVariableData)
	if e != nil {
		logDefault(chargePointID, localauth.GetLocalListVersionFeatureName).Errorf("couldn't send message: %v", e)
		return
	}

	// Wait for some time
	time.Sleep(5 * time.Second)
	// Trigger a heartbeat message
	cb5 := func(confirmation *remotecontrol.TriggerMessageResponse, err error) {
		if err != nil {
			logDefault(chargePointID, remotecontrol.TriggerMessageFeatureName).Errorf("error on request: %v", err)
		} else if confirmation.Status == remotecontrol.TriggerMessageStatusAccepted {
			logDefault(chargePointID, confirmation.GetFeatureName()).Infof("%v triggered successfully", availability.HeartbeatFeatureName)
		} else if confirmation.Status == remotecontrol.TriggerMessageStatusRejected {
			logDefault(chargePointID, confirmation.GetFeatureName()).Infof("%v trigger was rejected", availability.HeartbeatFeatureName)
		}
	}
	e = csms.TriggerMessage(chargePointID, cb5, core.HeartbeatFeatureName)
	if e != nil {
		logDefault(chargePointID, remotecontrol.TriggerMessageFeatureName).Errorf("couldn't send message: %v", e)
		return
	}

	// Wait for some time
	time.Sleep(5 * time.Second)
	// Trigger a diagnostics status notification
	cb6 := func(confirmation *remotecontrol.TriggerMessageResponse, err error) {
		if err != nil {
			logDefault(chargePointID, remotecontrol.TriggerMessageFeatureName).Errorf("error on request: %v", err)
		} else if confirmation.Status == remotecontrol.TriggerMessageStatusAccepted {
			logDefault(chargePointID, confirmation.GetFeatureName()).Infof("%v triggered successfully", diagnostics.LogStatusNotificationFeatureName)
		} else if confirmation.Status == remotecontrol.TriggerMessageStatusRejected {
			logDefault(chargePointID, confirmation.GetFeatureName()).Infof("%v trigger was rejected", diagnostics.LogStatusNotificationFeatureName)
		}
	}
	e = csms.TriggerMessage(chargePointID, cb6, diagnostics.LogStatusNotificationFeatureName)
	if e != nil {
		logDefault(chargePointID, remotecontrol.TriggerMessageFeatureName).Errorf("couldn't send message: %v", e)
		return
	}

	// Wait for some time
	time.Sleep(5 * time.Second)
	// Trigger a
}

// Start function
func main() {
	// Load config from ENV
	var listenPort = defaultListenPort
	port, _ := os.LookupEnv(envVarServerPort)
	if p, err := strconv.Atoi(port); err == nil {
		listenPort = p
	} else {
		log.Printf("no valid %v environment variable found, using default port", envVarServerPort)
	}
	// Check if TLS enabled
	t, _ := os.LookupEnv(envVarTls)
	tlsEnabled, _ := strconv.ParseBool(t)
	// Prepare OCPP 1.6 central system
	if tlsEnabled {
		csms = setupTlsCentralSystem()
	} else {
		csms = setupCentralSystem()
	}
	// Support callbacks for all OCPP 2.0.1 profiles
	handler := &CSMSHandler{chargingStations: map[string]*ChargingStationState{}}
	csms.SetAuthorizationHandler(handler)
	csms.SetAvailabilityHandler(handler)
	csms.SetDiagnosticsHandler(handler)
	csms.SetFirmwareHandler(handler)
	csms.SetLocalAuthListHandler(handler)
	csms.SetMeterHandler(handler)
	csms.SetProvisioningHandler(handler)
	csms.SetRemoteControlHandler(handler)
	csms.SetReservationHandler(handler)
	csms.SetTariffCostHandler(handler)
	csms.SetTransactionsHandler(handler)
	// Add handlers for dis/connection of charge points
	csms.SetNewChargingStationHandler(func(chargePoint ocpp2.ChargingStationConnection) {
		handler.chargingStations[chargePoint.ID()] = &ChargingStationState{connectors: map[int]*ConnectorInfo{}, transactions: map[int]*TransactionInfo{}}
		log.WithField("client", chargePoint.ID()).Info("new charging station connected")
		go exampleRoutine(chargePoint.ID(), handler)
	})
	csms.SetChargingStationDisconnectedHandler(func(chargePoint ocpp2.ChargingStationConnection) {
		log.WithField("client", chargePoint.ID()).Info("charging station disconnected")
		delete(handler.chargingStations, chargePoint.ID())
	})
	ocppj.SetLogger(log)
	// Run CSMS
	log.Infof("starting CSMS on port %v", listenPort)
	csms.Start(listenPort, "/{ws}")
	log.Info("stopped CSMS")
}

func init() {
	log = logrus.New()
	log.SetFormatter(&logrus.TextFormatter{FullTimestamp: true})
	log.SetLevel(logrus.InfoLevel)
}
