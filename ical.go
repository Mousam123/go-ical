// Package ical implements the iCalendar file format.
//
// iCalendar is defined in RFC 5545.
package ical

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// MIME type and file extension for iCal, defined in RFC 5545 section 8.1.
const (
	MIMEType  = "text/calendar"
	Extension = "ics"
)

// Params is a set of property parameters.
type Params map[string][]string

func (params Params) Get(name string) string {
	if values := params[strings.ToUpper(name)]; len(values) > 0 {
		return values[0]
	}
	return ""
}

func (params Params) Set(name, value string) {
	params[strings.ToUpper(name)] = []string{value}
}

func (params Params) Add(name, value string) {
	name = strings.ToUpper(name)
	params[name] = append(params[name], value)
}

func (params Params) Del(name string) {
	delete(params, strings.ToUpper(name))
}

// Prop is a component property.
type Prop struct {
	Name   string
	Params Params
	Value  string
}

// NewProp creates a new property with the specified name.
func NewProp(name string) *Prop {
	return &Prop{
		Name:   strings.ToUpper(name),
		Params: make(Params),
	}
}

func (prop *Prop) ValueType() ValueType {
	t := ValueType(prop.Params.Get(ParamValue))
	if t == ValueDefault {
		t = defaultValueTypes[prop.Name]
	}
	return t
}

func (prop *Prop) SetValueType(t ValueType) {
	dt, ok := defaultValueTypes[prop.Name]
	if t == ValueDefault || (ok && t == dt) {
		prop.Params.Del(ParamValue)
	} else {
		prop.Params.Set(ParamValue, string(t))
	}
}

func (prop *Prop) expectValueType(want ValueType) error {
	t := prop.ValueType()
	if t != ValueDefault && t != want {
		return fmt.Errorf("ical: property %q: expected type %q, got %q", prop.Name, want, t)
	}
	return nil
}

func (prop *Prop) Binary() ([]byte, error) {
	if err := prop.expectValueType(ValueBinary); err != nil {
		return nil, err
	}
	return base64.StdEncoding.DecodeString(prop.Value)
}

func (prop *Prop) SetBinary(b []byte) {
	prop.SetValueType(ValueBinary)
	prop.Value = base64.StdEncoding.EncodeToString(b)
}

func (prop *Prop) Bool() (bool, error) {
	if err := prop.expectValueType(ValueBool); err != nil {
		return false, err
	}
	switch strings.ToUpper(prop.Value) {
	case "TRUE":
		return true, nil
	case "FALSE":
		return false, nil
	default:
		return false, fmt.Errorf("ical: invalid boolean: %q", prop.Value)
	}
}

// DateTime parses the property value as a date-time or a date.
func (prop *Prop) DateTime(loc *time.Location) (time.Time, error) {
	if loc == nil {
		loc = time.UTC
	}
	switch t := prop.ValueType(); t {
	case ValueDefault, ValueDateTime:
		// TODO: use the TZID parameter, if any
		if t, err := time.ParseInLocation("20060102T150405", prop.Value, loc); err == nil {
			return t, nil
		}
		return time.ParseInLocation("20060102T150405Z", prop.Value, time.UTC)
	case ValueDate:
		return time.ParseInLocation("20060102", prop.Value, loc)
	default:
		return time.Time{}, fmt.Errorf("ical: expected DATE or DATE-TIME, got %q", t)
	}
}

func (prop *Prop) SetDateTime(t time.Time) {
	prop.SetValueType(ValueDateTime)
	prop.Value = t.Format("20060102T150405Z")
}

type durationParser struct {
	s string
}

func (p *durationParser) consume(c byte) bool {
	if len(p.s) == 0 || p.s[0] != c {
		return false
	}
	p.s = p.s[1:]
	return true
}

func (p *durationParser) parseCount() (time.Duration, error) {
	// Find the first non-digit
	i := strings.IndexFunc(p.s, func(r rune) bool {
		return r < '0' || r > '9'
	})
	if i == 0 {
		return 0, fmt.Errorf("ical: invalid duration: expected a digit")
	}
	if i < 0 {
		i = len(p.s)
	}

	n, err := strconv.ParseUint(p.s[:i], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("ical: invalid duration: %v", err)
	}
	p.s = p.s[i:]
	return time.Duration(n), nil
}

func (p *durationParser) parseDuration() (time.Duration, error) {
	neg := p.consume('-')
	if !neg {
		_ = p.consume('+')
	}

	if !p.consume('P') {
		return 0, fmt.Errorf("ical: invalid duration: expected 'P'")
	}

	var dur time.Duration
	isTime := false
	for len(p.s) > 0 {
		if p.consume('T') {
			isTime = true
		}

		n, err := p.parseCount()
		if err != nil {
			return 0, err
		}

		if !isTime {
			if p.consume('D') {
				dur += n * 24 * time.Hour
			} else if p.consume('W') {
				dur += n * 7 * 24 * time.Hour
			} else {
				return 0, fmt.Errorf("ical: invalid duration: expected 'D' or 'W'")
			}
		} else {
			if p.consume('H') {
				dur += n * time.Hour
			} else if p.consume('M') {
				dur += n * time.Minute
			} else if p.consume('S') {
				dur += n * time.Second
			} else {
				return 0, fmt.Errorf("ical: invalid duration: expected 'H', 'M' or 'S'")
			}
		}
	}

	if neg {
		dur = -dur
	}
	return dur, nil
}

func (prop *Prop) Duration() (time.Duration, error) {
	if err := prop.expectValueType(ValueDuration); err != nil {
		return 0, err
	}
	p := durationParser{strings.ToUpper(prop.Value)}
	return p.parseDuration()
}

func (prop *Prop) SetDuration(dur time.Duration) {
	prop.SetValueType(ValueDuration)

	sec := dur.Milliseconds() / 1000
	neg := sec < 0
	if sec < 0 {
		sec = -sec
	}

	var s string
	if neg {
		s += "-"
	}
	s += "PT"
	s += strconv.FormatInt(sec, 10)
	s += "S"

	prop.Value = s
}

func (prop *Prop) Float() (float64, error) {
	if err := prop.expectValueType(ValueFloat); err != nil {
		return 0, err
	}
	return strconv.ParseFloat(prop.Value, 64)
}

func (prop *Prop) Int() (int, error) {
	if err := prop.expectValueType(ValueInt); err != nil {
		return 0, err
	}
	return strconv.Atoi(prop.Value)
}

func (prop *Prop) TextList() ([]string, error) {
	if err := prop.expectValueType(ValueText); err != nil {
		return nil, err
	}

	var l []string
	var sb strings.Builder
	for i := 0; i < len(prop.Value); i++ {
		switch c := prop.Value[i]; c {
		case '\\':
			i++
			if i >= len(prop.Value) {
				return nil, fmt.Errorf("ical: malformed text: antislash at end of text")
			}
			switch c := prop.Value[i]; c {
			case '\\', ';', ',':
				sb.WriteByte(c)
			case 'n', 'N':
				sb.WriteByte('\n')
			default:
				return nil, fmt.Errorf("ical: malformed text: invalid escape sequence '\\%v'", c)
			}
		case ',':
			l = append(l, sb.String())
			sb.Reset()
		default:
			sb.WriteByte(c)
		}
	}
	l = append(l, sb.String())

	return l, nil
}

func (prop *Prop) SetTextList(l []string) {
	prop.SetValueType(ValueText)

	var sb strings.Builder
	for i, text := range l {
		if i > 0 {
			sb.WriteByte(',')
		}

		sb.Grow(len(text))
		for _, r := range text {
			switch r {
			case '\\', ';', ',':
				sb.WriteByte('\\')
				sb.WriteRune(r)
			case '\n':
				sb.WriteString("\\n")
			default:
				sb.WriteRune(r)
			}
		}
	}
	prop.Value = sb.String()
}

func (prop *Prop) Text() (string, error) {
	l, err := prop.TextList()
	if err != nil {
		return "", err
	}
	if len(l) == 0 {
		return "", nil
	}
	return l[0], nil
}

func (prop *Prop) SetText(text string) {
	prop.SetTextList([]string{text})
}

// TODO: Period, RecurrenceRule, Time, URI, UTCOffset

// Props is a set of component properties.
type Props map[string][]Prop

func (props Props) Get(name string) *Prop {
	if l := props[strings.ToUpper(name)]; len(l) > 0 {
		return &l[0]
	}
	return nil
}

func (props Props) Set(prop *Prop) {
	props[prop.Name] = []Prop{*prop}
}

func (props Props) Add(prop *Prop) {
	props[prop.Name] = append(props[prop.Name], *prop)
}

func (props Props) Del(name string) {
	delete(props, name)
}

func (props Props) Text(name string) (string, error) {
	if prop := props.Get(name); prop != nil {
		return prop.Text()
	}
	return "", nil
}

func (props Props) SetText(name, text string) {
	prop := NewProp(name)
	prop.SetText(text)
	props.Set(prop)
}

func (props Props) DateTime(name string, loc *time.Location) (time.Time, error) {
	if prop := props.Get(name); prop != nil {
		return prop.DateTime(loc)
	}
	return time.Time{}, nil
}

func (props Props) SetDateTime(name string, t time.Time) {
	prop := NewProp(name)
	prop.SetDateTime(t)
	props.Set(prop)
}

// Component is an iCalendar component: collections of properties that express
// a particular calendar semantic. A components can be an events, a to-do, a
// journal entry, timezone information, free/busy time information, or an
// alarm.
type Component struct {
	Name     string
	Props    Props
	Children []*Component
}

// NewComponent creates a new component with the specified name.
func NewComponent(name string) *Component {
	return &Component{
		Name:  strings.ToUpper(name),
		Props: make(Props),
	}
}

// Components as defined in RFC 5545 section 3.6.
const (
	CompCalendar = "VCALENDAR"
	CompEvent    = "VEVENT"
	CompToDo     = "VTODO"
	CompJournal  = "VJOURNAL"
	CompFreeBusy = "VFREEBUSY"
	CompTimezone = "VTIMEZONE"
	CompAlarm    = "VALARM"
)

// Timezone components.
const (
	CompTimezoneStandard = "STANDARD"
	CompTimezoneDaylight = "DAYLIGHT"
)

// Properties as defined in RFC 5545 section 3.7 and section 3.8.
const (
	// Calendar properties
	PropCalendarScale = "CALSCALE"
	PropMethod        = "METHOD"
	PropProductID     = "PRODID"
	PropVersion       = "VERSION"

	// Component properties
	PropAttach          = "ATTACH"
	PropCategories      = "CATEGORIES"
	PropClass           = "CLASS"
	PropComment         = "COMMENT"
	PropDescription     = "DESCRIPTION"
	PropGeo             = "GEO"
	PropLocation        = "LOCATION"
	PropPercentComplete = "PERCENT-COMPLETE"
	PropPriority        = "PRIORITY"
	PropResources       = "RESOURCES"
	PropStatus          = "STATUS"
	PropSummary         = "SUMMARY"

	// Date and time component properties
	PropCompleted     = "COMPLETED"
	PropDateTimeEnd   = "DTEND"
	PropDue           = "DUE"
	PropDateTimeStart = "DTSTART"
	PropDuration      = "DURATION"
	PropFreeBusy      = "FREEBUSY"
	PropTransparency  = "TRANSP"

	// Timezone component properties
	PropTimezoneID         = "TZID"
	PropTimezoneName       = "TZNAME"
	PropTimezoneOffsetFrom = "TZOFFSETFROM"
	PropTimezoneOffsetTo   = "TZOFFSETTO"
	PropTimezoneURL        = "TZURL"

	// Relationship component properties
	PropAttendee     = "ATTENDEE"
	PropContact      = "CONTACT"
	PropOrganizer    = "ORGANIZER"
	PropRecurrenceID = "RECURRENCE-ID"
	PropRelatedTo    = "RELATED-TO"
	PropURL          = "URL"
	PropUID          = "UID"

	// Recurrence component properties
	PropExceptionDates  = "EXDATE"
	PropRecurrenceDates = "RDATE"
	PropRecurrenceRule  = "RRULE"

	// Alarm component properties
	PropAction  = "ACTION"
	PropRepeat  = "REPEAT"
	PropTrigger = "TRIGGER"

	// Change management component properties
	PropCreated       = "CREATED"
	PropDateTimeStamp = "DTSTAMP"
	PropLastModified  = "LAST-MODIFIED"
	PropSequence      = "SEQUENCE"

	// Miscellaneous component properties
	PropRequestStatus = "REQUEST-STATUS"
)

// Property parameters as defined in RFC 5545 section 3.2.
const (
	ParamAltRep              = "ALTREP"
	ParamCommonName          = "CN"
	ParamCalendarUserType    = "CUTYPE"
	ParamDelegatedFrom       = "DELEGATED-FROM"
	ParamDelegatedTo         = "DELEGATED-TO"
	ParamDir                 = "DIR"
	ParamEncoding            = "ENCODING"
	ParamFormatType          = "FMTTYPE"
	ParamFreeBusyType        = "FBTYPE"
	ParamLanguage            = "LANGUAGE"
	ParamMember              = "MEMBER"
	ParamParticipationStatus = "PARTSTAT"
	ParamRange               = "RANGE"
	ParamRelated             = "RELATED"
	ParamRelationshipType    = "RELTYPE"
	ParamRole                = "ROLE"
	ParamRSVP                = "RSVP"
	ParamSentBy              = "SENT-BY"
	ParamTimezoneID          = "TZID"
	ParamValue               = "VALUE"
)

// ValueType is the type of a property.
type ValueType string

// Value types as defined in RFC 5545 section 3.3.
const (
	ValueDefault         ValueType = ""
	ValueBinary          ValueType = "BINARY"
	ValueBool            ValueType = "BOOLEAN"
	ValueCalendarAddress ValueType = "CAL-ADDRESS"
	ValueDate            ValueType = "DATE"
	ValueDateTime        ValueType = "DATE-TIME"
	ValueDuration        ValueType = "DURATION"
	ValueFloat           ValueType = "FLOAT"
	ValueInt             ValueType = "INTEGER"
	ValuePeriod          ValueType = "PERIOD"
	ValueRecurrence      ValueType = "RECUR"
	ValueText            ValueType = "TEXT"
	ValueTime            ValueType = "TIME"
	ValueURI             ValueType = "URI"
	ValueUTCOffset       ValueType = "UTC-OFFSET"
)

var defaultValueTypes = map[string]ValueType{
	PropCalendarScale:      ValueText,
	PropMethod:             ValueText,
	PropProductID:          ValueText,
	PropVersion:            ValueText,
	PropAttach:             ValueURI, // can be binary
	PropCategories:         ValueText,
	PropClass:              ValueText,
	PropComment:            ValueText,
	PropDescription:        ValueText,
	PropGeo:                ValueFloat,
	PropLocation:           ValueText,
	PropPercentComplete:    ValueInt,
	PropPriority:           ValueInt,
	PropResources:          ValueText,
	PropStatus:             ValueText,
	PropSummary:            ValueText,
	PropCompleted:          ValueDateTime,
	PropDateTimeEnd:        ValueDateTime, // can be date
	PropDue:                ValueDateTime, // can be date
	PropDateTimeStart:      ValueDateTime, // can be date
	PropDuration:           ValueDuration,
	PropFreeBusy:           ValuePeriod,
	PropTransparency:       ValueText,
	PropTimezoneID:         ValueText,
	PropTimezoneName:       ValueText,
	PropTimezoneOffsetFrom: ValueUTCOffset,
	PropTimezoneOffsetTo:   ValueUTCOffset,
	PropTimezoneURL:        ValueURI,
	PropAttendee:           ValueCalendarAddress,
	PropContact:            ValueText,
	PropOrganizer:          ValueCalendarAddress,
	PropRecurrenceID:       ValueDateTime, // can be date
	PropRelatedTo:          ValueText,
	PropURL:                ValueURI,
	PropUID:                ValueText,
	PropExceptionDates:     ValueDateTime, // can be date
	PropRecurrenceDates:    ValueDateTime, // can be date or period
	PropRecurrenceRule:     ValueRecurrence,
	PropAction:             ValueText,
	PropRepeat:             ValueInt,
	PropTrigger:            ValueDuration, // can be date-time
	PropCreated:            ValueDateTime,
	PropDateTimeStamp:      ValueDateTime,
	PropLastModified:       ValueDateTime,
	PropSequence:           ValueInt,
	PropRequestStatus:      ValueText,
}

// Calendar is the top-level iCalendar object.
type Calendar struct {
	*Component
}

// NewCalendar creates a new calendar object.
func NewCalendar() *Calendar {
	return &Calendar{NewComponent(CompCalendar)}
}

// Events extracts the list of events contained in the calendar.
func (cal *Calendar) Events() []Event {
	l := make([]Event, 0, len(cal.Children))
	for _, child := range cal.Children {
		if child.Name == CompEvent {
			l = append(l, Event{child})
		}
	}
	return l
}

type EventStatus string

const (
	EventTentative EventStatus = "TENTATIVE"
	EventConfirmed EventStatus = "CONFIRMED"
	EventCancelled EventStatus = "CANCELLED"
)

// Event represents a scheduled amount of time on a calendar.
type Event struct {
	*Component
}

// NewEvent creates a new event.
func NewEvent() *Event {
	return &Event{NewComponent(CompEvent)}
}

// DateTimeStart returns the inclusive start of the event.
func (e *Event) DateTimeStart(loc *time.Location) (time.Time, error) {
	return e.Props.DateTime(PropDateTimeStart, loc)
}

// DateTimeEnd returns the non-inclusive end of the event.
func (e *Event) DateTimeEnd(loc *time.Location) (time.Time, error) {
	if prop := e.Props.Get(PropDateTimeEnd); prop != nil {
		return prop.DateTime(loc)
	}

	startProp := e.Props.Get(PropDateTimeStart)
	if startProp == nil {
		return time.Time{}, nil
	}

	start, err := startProp.DateTime(loc)
	if err != nil {
		return time.Time{}, err
	}

	var dur time.Duration
	if durProp := e.Props.Get(PropDuration); durProp != nil {
		dur, err = durProp.Duration()
		if err != nil {
			return time.Time{}, err
		}
	} else if startProp.ValueType() == ValueDate {
		dur = 24 * time.Hour
	}

	return start.Add(dur), nil
}

func (e *Event) Status() (EventStatus, error) {
	s, err := e.Props.Text(PropStatus)
	if err != nil {
		return "", err
	}

	switch status := EventStatus(strings.ToUpper(s)); status {
	case "", EventTentative, EventConfirmed, EventCancelled:
		return status, nil
	default:
		return "", fmt.Errorf("ical: invalid VEVENT STATUS: %q", status)
	}
}

func (e *Event) SetStatus(status EventStatus) {
	if status == "" {
		e.Props.Del(PropStatus)
	} else {
		e.Props.SetText(PropStatus, string(status))
	}
}
