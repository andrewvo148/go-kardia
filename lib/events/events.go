/*
 *  Copyright 2018 KardiaChain
 *  This file is part of the go-kardia library.
 *
 *  The go-kardia library is free software: you can redistribute it and/or modify
 *  it under the terms of the GNU Lesser General Public License as published by
 *  the Free Software Foundation, either version 3 of the License, or
 *  (at your option) any later version.
 *
 *  The go-ethereum library is distributed in the hope that it will be useful,
 *  but WITHOUT ANY WARRANTY; without even the implied warranty of
 *  MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
 *  GNU Lesser General Public License for more details.
 *
 *  You should have received a copy of the GNU Lesser General Public License
 *  along with the go-kardia library. If not, see <http://www.gnu.org/licenses/>.
 */

/* Pubsub library. */
package events

import (
	"sync"
)

// Data passing from pub to sub.
type EventData interface {
}

//-------- Event switch ---------
type EventSwitch interface {
	// TODO(namdoh): Adds interface for start/stop/etc. of event bus.

	AddListenerForEvent(listenerID, event string, cb EventCallback)
	RemoveListenerForEvent(event string, listenerID string)
	RemoveListener(listenerID string)
	FireEvent(event string, data EventData)
}

type eventSwitch struct {
	mtx        sync.RWMutex
	eventCells map[string]*eventCell
	listeners  map[string]*eventListener
}

func NewEventSwitch() EventSwitch {
	evsw := &eventSwitch{
		eventCells: make(map[string]*eventCell),
		listeners:  make(map[string]*eventListener),
	}
	return evsw
}

func (evsw *eventSwitch) AddListenerForEvent(listenerID, event string, cb EventCallback) {
	// Get/Create eventCell and listener
	evsw.mtx.Lock()
	eventCell := evsw.eventCells[event]
	if eventCell == nil {
		eventCell = newEventCell()
		evsw.eventCells[event] = eventCell
	}
	listener := evsw.listeners[listenerID]
	if listener == nil {
		listener = newEventListener(listenerID)
		evsw.listeners[listenerID] = listener
	}
	evsw.mtx.Unlock()

	// Add event and listener
	eventCell.AddListener(listenerID, cb)
	listener.AddEvent(event)
}

func (evsw *eventSwitch) RemoveListener(listenerID string) {
	// Get and remove listener
	evsw.mtx.RLock()
	listener := evsw.listeners[listenerID]
	evsw.mtx.RUnlock()
	if listener == nil {
		return
	}

	evsw.mtx.Lock()
	delete(evsw.listeners, listenerID)
	evsw.mtx.Unlock()

	// Remove callback for each event.
	listener.SetRemoved()
	for _, event := range listener.GetEvents() {
		evsw.RemoveListenerForEvent(event, listenerID)
	}
}

func (evsw *eventSwitch) RemoveListenerForEvent(event string, listenerID string) {
	// Get eventCell
	evsw.mtx.Lock()
	eventCell := evsw.eventCells[event]
	evsw.mtx.Unlock()

	if eventCell == nil {
		return
	}

	// Remove listenerID from eventCell
	numListeners := eventCell.RemoveListener(listenerID)

	// Maybe garbage collect eventCell.
	if numListeners == 0 {
		// Lock again and double check.
		evsw.mtx.Lock()      // OUTER LOCK
		eventCell.mtx.Lock() // INNER LOCK
		if len(eventCell.listeners) == 0 {
			delete(evsw.eventCells, event)
		}
		eventCell.mtx.Unlock() // INNER LOCK
		evsw.mtx.Unlock()      // OUTER LOCK
	}
}

func (evsw *eventSwitch) FireEvent(event string, data EventData) {
	// Get the eventCell
	evsw.mtx.RLock()
	eventCell := evsw.eventCells[event]
	evsw.mtx.RUnlock()

	if eventCell == nil {
		return
	}

	// Fire event for all listeners in eventCell
	eventCell.FireEvent(data)
}

// --------- Event cell ----------

// eventCell handles keeping track of listener callbacks for a given event.
type eventCell struct {
	mtx       sync.RWMutex
	listeners map[string]EventCallback
}

func newEventCell() *eventCell {
	return &eventCell{
		listeners: make(map[string]EventCallback),
	}
}

func (cell *eventCell) AddListener(listenerID string, cb EventCallback) {
	cell.mtx.Lock()
	cell.listeners[listenerID] = cb
	cell.mtx.Unlock()
}

func (cell *eventCell) RemoveListener(listenerID string) int {
	cell.mtx.Lock()
	delete(cell.listeners, listenerID)
	numListeners := len(cell.listeners)
	cell.mtx.Unlock()
	return numListeners
}

func (cell *eventCell) FireEvent(data EventData) {
	cell.mtx.RLock()
	for _, listener := range cell.listeners {
		listener(data)
	}
	cell.mtx.RUnlock()
}

// -------- Event callback ---------

type EventCallback func(data EventData)

type eventListener struct {
	id string

	mtx     sync.RWMutex
	removed bool
	events  []string
}

func newEventListener(id string) *eventListener {
	return &eventListener{
		id:      id,
		removed: false,
		events:  nil,
	}
}

func (evl *eventListener) AddEvent(event string) {
	evl.mtx.Lock()
	defer evl.mtx.Unlock()

	if evl.removed {
		return
	}
	evl.events = append(evl.events, event)
}

func (evl *eventListener) GetEvents() []string {
	evl.mtx.RLock()
	defer evl.mtx.RUnlock()

	events := make([]string, len(evl.events))
	copy(events, evl.events)
	return events
}

func (evl *eventListener) SetRemoved() {
	evl.mtx.Lock()
	defer evl.mtx.Unlock()
	evl.removed = true
}
