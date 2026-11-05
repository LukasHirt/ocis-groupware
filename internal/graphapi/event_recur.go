package graphapi

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/LukasHirt/groupkit/ical"
)

var (
	dayNames = []string{"sunday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday"}
	dayCodes = []string{"SU", "MO", "TU", "WE", "TH", "FR", "SA"}
	indexes  = map[string]string{"first": "1", "second": "2", "third": "3", "fourth": "4", "last": "-1"}

	byDayRE = regexp.MustCompile(`^([+-]?[0-9]{1,2})?(SU|MO|TU|WE|TH|FR|SA)$`)
)

func dayName(code string) string {
	for i, c := range dayCodes {
		if c == code {
			return dayNames[i]
		}
	}
	return ""
}

func dayCode(name string) string {
	for i, n := range dayNames {
		if n == strings.ToLower(name) {
			return dayCodes[i]
		}
	}
	return ""
}

func indexName(n string) string {
	for name, v := range indexes {
		if v == n {
			return name
		}
	}
	return ""
}

// ruleParts splits an RRULE into its parts. It fails on a repeated part.
func ruleParts(rule string) (map[string]string, bool) {
	parts := map[string]string{}
	for _, kv := range strings.Split(rule, ";") {
		k, v, ok := strings.Cut(kv, "=")
		k = strings.ToUpper(k)
		if _, dup := parts[k]; !ok || dup || v == "" {
			return nil, false
		}
		parts[k] = strings.ToUpper(v)
	}
	return parts, true
}

// recurrence maps the recurrence of a series master. The second result is
// false when the series has a rule the Graph pattern cannot express; the
// event then has no recurrence and clients must not edit it.
func (v eventView) recurrence(c *ical.Component) (*recurrenceJSON, *bool) {
	notEditable := func() (*recurrenceJSON, *bool) {
		no := false
		return nil, &no
	}
	rules := c.PropAll("RRULE")
	if len(rules) != 1 || c.Prop("EXRULE") != nil {
		return notEditable()
	}
	p := c.Prop("DTSTART")
	if p == nil {
		return notEditable()
	}
	start, err := ical.ParseDateTime(p.Value, p.Params)
	if err != nil {
		return notEditable()
	}
	parts, ok := ruleParts(rules[0].Value)
	if !ok {
		return notEditable()
	}
	r, ok := v.patternOf(parts, start)
	if !ok {
		return notEditable()
	}
	return r, nil
}

func (v eventView) patternOf(parts map[string]string, start ical.Value) (*recurrenceJSON, bool) {
	for k := range parts {
		switch k {
		case "FREQ", "INTERVAL", "COUNT", "UNTIL", "BYDAY", "BYMONTHDAY", "BYMONTH", "BYSETPOS", "WKST":
		default:
			return nil, false
		}
	}
	if _, both := parts["COUNT"]; both {
		if _, until := parts["UNTIL"]; until {
			return nil, false
		}
	}
	pat := recurrencePattern{Interval: 1, DaysOfWeek: []string{}, FirstDayOfWeek: "sunday", Index: "first"}
	if s, ok := parts["INTERVAL"]; ok {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > 99 {
			return nil, false
		}
		pat.Interval = n
	}
	byDay, byMonthDay, byMonth, bySetPos := parts["BYDAY"], parts["BYMONTHDAY"], parts["BYMONTH"], parts["BYSETPOS"]
	startDay := dayNames[start.Wall.Weekday()]
	switch parts["FREQ"] {
	case "DAILY":
		if byDay != "" || byMonthDay != "" || byMonth != "" || bySetPos != "" || parts["WKST"] != "" {
			return nil, false
		}
		pat.Type = "daily"
	case "WEEKLY":
		if byMonthDay != "" || byMonth != "" || bySetPos != "" {
			return nil, false
		}
		pat.Type = "weekly"
		pat.FirstDayOfWeek = "monday"
		if w := parts["WKST"]; w != "" {
			if pat.FirstDayOfWeek = dayName(w); pat.FirstDayOfWeek == "" {
				return nil, false
			}
		}
		if byDay == "" {
			pat.DaysOfWeek = []string{startDay}
			break
		}
		days, _, ok := parseByDay(byDay, false)
		if !ok {
			return nil, false
		}
		pat.DaysOfWeek = days
	case "MONTHLY":
		if byMonth != "" || parts["WKST"] != "" {
			return nil, false
		}
		if !v.monthDay(&pat, "absoluteMonthly", "relativeMonthly", byDay, byMonthDay, bySetPos, start) {
			return nil, false
		}
	case "YEARLY":
		if parts["WKST"] != "" {
			return nil, false
		}
		pat.Month = int(start.Wall.Month())
		if byMonth == "" && (byDay != "" || byMonthDay != "" || bySetPos != "") {
			return nil, false
		}
		if byMonth != "" {
			n, err := strconv.Atoi(byMonth)
			if err != nil || n < 1 || n > 12 {
				return nil, false
			}
			pat.Month = n
		}
		if !v.monthDay(&pat, "absoluteYearly", "relativeYearly", byDay, byMonthDay, bySetPos, start) {
			return nil, false
		}
	default:
		return nil, false
	}

	rng := recurrenceRange{Type: "noEnd", StartDate: start.Wall.Format("2006-01-02"), RecurrenceTimeZone: "UTC"}
	if start.TZID != "" {
		rng.RecurrenceTimeZone = start.TZID
	}
	switch {
	case parts["COUNT"] != "":
		n, err := strconv.Atoi(parts["COUNT"])
		if err != nil || n < 1 {
			return nil, false
		}
		rng.Type, rng.NumberOfOccurrences = "numbered", n
	case parts["UNTIL"] != "":
		u, err := ical.ParseDateTime(parts["UNTIL"], nil)
		if err != nil {
			return nil, false
		}
		wall := u.Wall
		if u.UTC && start.TZID != "" {
			wall = v.res.WallOf(u.Wall, start.TZID, nil)
		}
		rng.Type, rng.EndDate = "endDate", wall.Format("2006-01-02")
	}
	return &recurrenceJSON{Pattern: pat, Range: rng}, true
}

// monthDay fills the day part of a monthly or yearly pattern: a day of the
// month, or the nth weekday, or the nth of a set of weekdays.
func (v eventView) monthDay(pat *recurrencePattern, abs, rel, byDay, byMonthDay, bySetPos string, start ical.Value) bool {
	switch {
	case byDay == "" && bySetPos == "":
		pat.Type = abs
		pat.DayOfMonth = start.Wall.Day()
		if byMonthDay != "" {
			n, err := strconv.Atoi(byMonthDay)
			if err != nil || n < 1 || n > 31 {
				return false
			}
			pat.DayOfMonth = n
		}
		return true
	case byDay == "" || byMonthDay != "":
		return false
	}
	days, ord, ok := parseByDay(byDay, true)
	if !ok {
		return false
	}
	pat.Type, pat.DaysOfWeek = rel, days
	switch {
	case len(days) == 1 && ord != "" && bySetPos == "":
		pat.Index = indexName(ord)
	case ord == "" && bySetPos != "":
		pat.Index = indexName(bySetPos)
	default:
		return false
	}
	return pat.Index != ""
}

// parseByDay reads a BYDAY list. With ordinals allowed, a single day may
// carry one; the second result is that ordinal.
func parseByDay(s string, ordinals bool) (days []string, ord string, ok bool) {
	items := strings.Split(s, ",")
	for _, it := range items {
		m := byDayRE.FindStringSubmatch(it)
		if m == nil {
			return nil, "", false
		}
		if m[1] != "" {
			if !ordinals || len(items) > 1 {
				return nil, "", false
			}
			ord = strings.TrimPrefix(m[1], "+")
		}
		days = append(days, dayName(m[2]))
	}
	return days, ord, true
}

// buildRule turns a Graph recurrence into an RRULE value. start is the
// event's first start, in the zone the rule runs in.
func buildRule(res *ical.Resolver, r recurrenceJSON, start zonedTime, allDay bool) (string, *apiError) {
	p := r.Pattern
	if p.Interval < 1 || p.Interval > 99 {
		return "", badRequest("recurrence interval must be from 1 to 99")
	}
	var freq string
	switch p.Type {
	case "daily":
		freq = "DAILY"
	case "weekly":
		freq = "WEEKLY"
	case "absoluteMonthly", "relativeMonthly":
		freq = "MONTHLY"
	case "absoluteYearly", "relativeYearly":
		freq = "YEARLY"
	default:
		return "", badRequest("unknown recurrence pattern type")
	}
	rule := "FREQ=" + freq
	if p.Interval > 1 {
		rule += ";INTERVAL=" + strconv.Itoa(p.Interval)
	}

	switch r.Range.Type {
	case "noEnd":
	case "numbered":
		if r.Range.NumberOfOccurrences < 1 || r.Range.NumberOfOccurrences > 999 {
			return "", badRequest("numberOfOccurrences must be from 1 to 999")
		}
		rule += ";COUNT=" + strconv.Itoa(r.Range.NumberOfOccurrences)
	case "endDate":
		end, err := time.Parse("2006-01-02", r.Range.EndDate)
		if err != nil {
			return "", badRequest("endDate must look like 2026-12-31")
		}
		if allDay {
			rule += ";UNTIL=" + end.Format("20060102")
			break
		}
		last := start
		last.wall = end.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
		rule += ";UNTIL=" + instant(res, last).Format("20060102T150405Z")
	default:
		return "", badRequest("unknown recurrence range type")
	}

	var days []string
	for _, d := range p.DaysOfWeek {
		code := dayCode(d)
		if code == "" {
			return "", badRequest("unknown day of week " + clip(d))
		}
		days = append(days, code)
	}
	month := func() *apiError {
		if p.Month < 1 || p.Month > 12 {
			return badRequest("month must be from 1 to 12")
		}
		rule += ";BYMONTH=" + strconv.Itoa(p.Month)
		return nil
	}
	dom := func() *apiError {
		if p.DayOfMonth < 1 || p.DayOfMonth > 31 {
			return badRequest("dayOfMonth must be from 1 to 31")
		}
		rule += ";BYMONTHDAY=" + strconv.Itoa(p.DayOfMonth)
		return nil
	}
	nth := func() *apiError {
		idx, ok := indexes[strings.ToLower(p.Index)]
		if p.Index == "" {
			idx, ok = "1", true
		}
		if !ok || len(days) == 0 {
			return badRequest("a relative pattern needs daysOfWeek and an index from first to last")
		}
		if len(days) == 1 {
			rule += ";BYDAY=" + idx + days[0]
			return nil
		}
		rule += ";BYDAY=" + strings.Join(days, ",") + ";BYSETPOS=" + idx
		return nil
	}
	var aerr *apiError
	switch p.Type {
	case "weekly":
		if len(days) == 0 {
			days = []string{dayCodes[start.wall.Weekday()]}
		}
		rule += ";BYDAY=" + strings.Join(days, ",")
		if w := strings.ToLower(p.FirstDayOfWeek); w != "" && dayCode(w) != "" && w != "monday" {
			rule += ";WKST=" + dayCode(w)
		}
	case "absoluteMonthly":
		aerr = dom()
	case "relativeMonthly":
		aerr = nth()
	case "absoluteYearly":
		if aerr = month(); aerr == nil {
			aerr = dom()
		}
	case "relativeYearly":
		if aerr = month(); aerr == nil {
			aerr = nth()
		}
	}
	if aerr != nil {
		return "", aerr
	}
	return rule, nil
}
