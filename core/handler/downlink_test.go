// Copyright © 2017 The Things Network
// Use of this source code is governed by the MIT license that can be found in the LICENSE file.

package handler

import (
	"testing"
	"time"

	pb_broker "github.com/TheThingsNetwork/ttn/api/broker"
	"github.com/TheThingsNetwork/ttn/core/component"
	"github.com/TheThingsNetwork/ttn/core/handler/application"
	"github.com/TheThingsNetwork/ttn/core/handler/device"
	"github.com/TheThingsNetwork/ttn/core/types"
	. "github.com/TheThingsNetwork/ttn/utils/testing"
	. "github.com/smartystreets/assertions"
)

func TestEnqueueDownlink(t *testing.T) {
	a := New(t)
	appID := "app1"
	devID := "dev1"
	h := &handler{
		Component: &component.Component{Ctx: GetLogger(t, "TestEnqueueDownlink")},
		devices:   device.NewRedisDeviceStore(GetRedisClient(), "handler-test-enqueue-downlink"),
		mqttEvent: make(chan *types.DeviceEvent, 10),
	}
	err := h.EnqueueDownlink(&types.DownlinkMessage{
		AppID: appID,
		DevID: devID,
	})
	a.So(err, ShouldNotBeNil)
	h.devices.Set(&device.Device{
		AppID: appID,
		DevID: devID,
	})
	defer func() {
		h.devices.Delete(appID, devID)
	}()
	queue, _ := h.devices.DownlinkQueue(appID, devID)

	err = h.EnqueueDownlink(&types.DownlinkMessage{
		AppID:      appID,
		DevID:      devID,
		PayloadRaw: []byte{0x01},
		Schedule:   "last",
	})
	a.So(err, ShouldBeNil)
	qLen, _ := queue.Length()
	a.So(qLen, ShouldEqual, 1)

	err = h.EnqueueDownlink(&types.DownlinkMessage{
		AppID:      appID,
		DevID:      devID,
		PayloadRaw: []byte{0x02},
		Schedule:   "first",
	})
	a.So(err, ShouldBeNil)
	qLen, _ = queue.Length()
	a.So(qLen, ShouldEqual, 2)

	err = h.EnqueueDownlink(&types.DownlinkMessage{
		AppID:    appID,
		DevID:    devID,
		Schedule: "random",
	})
	a.So(err, ShouldNotBeNil)

	err = h.EnqueueDownlink(&types.DownlinkMessage{
		AppID:    appID,
		DevID:    devID,
		Schedule: "replace",
		PayloadFields: map[string]interface{}{
			"string": "hello!",
			"int":    42,
			"bool":   true,
		},
	})
	a.So(err, ShouldBeNil)
	qLen, _ = queue.Length()
	a.So(qLen, ShouldEqual, 1)

	downlink, _ := queue.Next()
	a.So(downlink, ShouldNotBeNil)
	a.So(downlink.PayloadFields, ShouldHaveLength, 3)
}

func TestHandleDownlink(t *testing.T) {
	a := New(t)
	var err error
	var wg WaitGroup
	appID := "app2"
	devID := "dev2"
	appEUI := types.AppEUI([8]byte{1, 2, 3, 4, 5, 6, 7, 8})
	devEUI := types.DevEUI([8]byte{1, 2, 3, 4, 5, 6, 7, 8})
	h := &handler{
		Component:    &component.Component{Ctx: GetLogger(t, "TestHandleDownlink")},
		devices:      device.NewRedisDeviceStore(GetRedisClient(), "handler-test-handle-downlink"),
		applications: application.NewRedisApplicationStore(GetRedisClient(), "handler-test-enqueue-downlink"),
		downlink:     make(chan *pb_broker.DownlinkMessage),
		mqttEvent:    make(chan *types.DeviceEvent, 10),
	}
	h.InitStatus()
	// Neither payload nor Fields provided : ERROR
	err = h.HandleDownlink(&types.DownlinkMessage{
		AppID: appID,
		DevID: devID,
	}, &pb_broker.DownlinkMessage{
		AppEui: &appEUI,
		DevEui: &devEUI,
	})
	a.So(err, ShouldNotBeNil)

	h.devices.Set(&device.Device{
		AppID: appID,
		DevID: devID,
	})
	defer func() {
		h.devices.Delete(appID, devID)
	}()
	err = h.HandleDownlink(&types.DownlinkMessage{
		AppID: appID,
		DevID: devID,
	}, &pb_broker.DownlinkMessage{
		AppEui:  &appEUI,
		DevEui:  &devEUI,
		Payload: []byte{96, 4, 3, 2, 1, 0, 1, 0, 1, 0, 0, 0, 0},
	})
	a.So(err, ShouldBeNil)

	// Payload provided
	wg.Add(1)
	go func() {
		dl := <-h.downlink
		a.So(dl.Payload, ShouldNotBeEmpty)
		wg.Done()
	}()
	err = h.HandleDownlink(&types.DownlinkMessage{
		AppID:      appID,
		DevID:      devID,
		PayloadRaw: []byte{0xAA, 0xBC},
	}, &pb_broker.DownlinkMessage{
		AppEui:         &appEUI,
		DevEui:         &devEUI,
		Payload:        []byte{96, 4, 3, 2, 1, 0, 1, 0, 1, 0, 0, 0, 0},
		DownlinkOption: &pb_broker.DownlinkOption{},
	})
	a.So(err, ShouldBeNil)
	wg.WaitFor(100 * time.Millisecond)

	// Both Payload and Fields provided
	h.applications.Set(&application.Application{
		AppID: appID,
		Encoder: `function Encoder (payload){
	  		return [96, 4, 3, 2, 1, 0, 1, 0, 1, 0, 0, 0, 0]
			}`,
	})
	defer func() {
		h.applications.Delete(appID)
	}()
	jsonFields := map[string]interface{}{"temperature": 11}
	err = h.HandleDownlink(&types.DownlinkMessage{
		FPort:         1,
		AppID:         appID,
		DevID:         devID,
		PayloadFields: jsonFields,
		PayloadRaw:    []byte{0xAA, 0xBC},
	}, &pb_broker.DownlinkMessage{
		AppEui:  &appEUI,
		DevEui:  &devEUI,
		Payload: []byte{96, 4, 3, 2, 1, 0, 1, 0, 1, 0, 0, 0, 0},
	})
	a.So(err, ShouldNotBeNil)

	// JSON Fields provided
	wg.Add(1)
	go func() {
		dl := <-h.downlink
		a.So(dl.Payload, ShouldNotBeEmpty)
		wg.Done()
	}()
	err = h.HandleDownlink(&types.DownlinkMessage{
		FPort:         1,
		AppID:         appID,
		DevID:         devID,
		PayloadFields: jsonFields,
	}, &pb_broker.DownlinkMessage{
		AppEui:         &appEUI,
		DevEui:         &devEUI,
		Payload:        []byte{96, 4, 3, 2, 1, 0, 1, 0, 1, 0, 0, 0, 0},
		DownlinkOption: &pb_broker.DownlinkOption{},
	})
	a.So(err, ShouldBeNil)
	wg.WaitFor(100 * time.Millisecond)
}
