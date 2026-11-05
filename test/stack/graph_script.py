# SPDX-FileCopyrightText: 2026 ownCloud GmbH - A Kiteworks Company and ownCloud contributors
# SPDX-License-Identifier: Apache-2.0
"""Lists and creates events and contacts through the Graph API with plain requests.

This is the script a developer would write first, with no Graph SDK. It checks
the calls an integration needs: creating an event and a contact, reading them
back, paging, selecting fields, ordering, filtering, changing with If-Match,
and cleaning up. It leaves nothing behind when it passes.

Environment:
  GRAPH_URL   the base URL of the Graph API, for example https://ocis.localhost/graph/v1.0
  GRAPH_TOKEN a bearer token of the user to act as
  REQUESTS_CA_BUNDLE  optional, a PEM file for a self-signed certificate
"""

import os
import sys
import uuid

import requests

BASE = os.environ["GRAPH_URL"].rstrip("/")
session = requests.Session()
session.headers["Authorization"] = "Bearer " + os.environ["GRAPH_TOKEN"]

# A new run must not meet what an earlier one left, and two runs must not meet
# each other.
TAG = uuid.uuid4().hex[:8]
created = []


def call(method, path, expect, **kwargs):
    r = session.request(method, BASE + path, timeout=30, **kwargs)
    if r.status_code != expect:
        sys.exit("%s %s: %d, wanted %d: %s" % (method, path, r.status_code, expect, r.text[:300]))
    return r


def check(cond, message):
    if not cond:
        sys.exit("check failed: " + message)


def cleanup():
    for path in reversed(created):
        r = session.delete(BASE + path, timeout=30)
        if r.status_code not in (204, 404):
            print("cleanup %s: %d" % (path, r.status_code), file=sys.stderr)


def event(subject, start, end):
    return {
        "subject": subject,
        "start": {"dateTime": start, "timeZone": "UTC"},
        "end": {"dateTime": end, "timeZone": "UTC"},
    }


def main():
    # The user has a default calendar and an address book.
    cals = call("GET", "/me/calendars", 200).json()["value"]
    check(any(c["isDefaultCalendar"] for c in cals), "no default calendar")
    books = call("GET", "/me/contactFolders", 200).json()["value"]
    check(any(b["groupware.isDefault"] for b in books), "no default contact folder")

    # Create three events.
    ids = {}
    for name, day in (("Charlie", "05"), ("alpha", "07"), ("Bravo", "06")):
        subject = "%s %s" % (name, TAG)
        r = call("POST", "/me/events", 201, json=event(subject, "2031-03-%sT09:00:00" % day, "2031-03-%sT10:00:00" % day))
        ev = r.json()
        created.append("/me/events/" + ev["id"])
        ids[name] = ev["id"]
        check(ev["subject"] == subject and ev["@odata.etag"], "created event is incomplete")

    # Read one back, with the fields a client asked for.
    one = call("GET", "/me/events/" + ids["alpha"], 200, params={"$select": "subject,start"}).json()
    check(set(one) == {"id", "@odata.etag", "subject", "start"}, "select on one event: %s" % sorted(one))

    # Filter, order, page and select on the list.
    flt = "contains(subject,'%s') and start/dateTime ge '2031-03-01T00:00:00Z'" % TAG
    page = call("GET", "/me/events", 200, params={"$filter": flt, "$orderby": "subject desc", "$top": "2", "$select": "subject"}).json()
    names = [e["subject"].split()[0] for e in page["value"]]
    check(names == ["Charlie", "Bravo"], "first page: %s" % names)
    check("@odata.nextLink" in page, "no next link")
    nxt = session.get(page["@odata.nextLink"], timeout=30)
    check(nxt.status_code == 200, "next link: %d" % nxt.status_code)
    check([e["subject"].split()[0] for e in nxt.json()["value"]] == ["alpha"], "second page")

    by_start = call("GET", "/me/events", 200, params={"$filter": "contains(subject,'%s')" % TAG, "$orderby": "start/dateTime"}).json()["value"]
    check([e["subject"].split()[0] for e in by_start] == ["Charlie", "Bravo", "alpha"], "order by start")

    # The calendar view returns the same events in a window.
    view = call("GET", "/me/calendarView", 200, params={
        "startDateTime": "2031-03-01T00:00:00Z", "endDateTime": "2031-04-01T00:00:00Z",
        "$filter": "contains(subject,'%s')" % TAG}).json()["value"]
    check(len(view) == 3, "calendarView: %d events" % len(view))

    # Change an event, with and without a matching ETag.
    ev = call("GET", "/me/events/" + ids["Bravo"], 200).json()
    call("PATCH", "/me/events/" + ids["Bravo"], 412, headers={"If-Match": 'W/"stale"'}, json={"subject": "nope"})
    patched = call("PATCH", "/me/events/" + ids["Bravo"], 200, headers={"If-Match": ev["@odata.etag"]}, json={"location": {"displayName": "Room 4"}}).json()
    check(patched["location"]["displayName"] == "Room 4" and patched["subject"] == ev["subject"], "patch kept the other fields")

    # What the service does not offer says so.
    call("GET", "/me/events", 501, params={"$expand": "attachments"})
    call("GET", "/me/contacts", 501, params={"$filter": "displayName eq 'x'"})

    # Contacts: create two, read, order, select, change, delete.
    cids = {}
    for given, sur in (("Zed", "Adams"), ("Ann", "Zimmer")):
        body = {
            "givenName": given, "surname": sur, "displayName": "%s %s %s" % (given, sur, TAG),
            "companyName": "Initech", "emailAddresses": [{"address": "%s.%s@example.org" % (given.lower(), TAG)}],
            "mobilePhone": "+41 79 000 00 00",
        }
        c = call("POST", "/me/contacts", 201, json=body).json()
        created.append("/me/contacts/" + c["id"])
        cids[given] = c["id"]
        check(c["emailAddresses"][0]["address"].startswith(given.lower()), "contact email")

    mine = call("GET", "/me/contacts", 200, params={"$orderby": "surname desc", "$select": "displayName,surname", "$top": "1000"}).json()["value"]
    mine = [c for c in mine if TAG in c["displayName"]]
    check([c["surname"] for c in mine] == ["Zimmer", "Adams"], "contacts by surname: %s" % mine)
    check(set(mine[0]) == {"id", "@odata.etag", "displayName", "surname"}, "contact select: %s" % sorted(mine[0]))

    cont = call("GET", "/me/contacts/" + cids["Ann"], 200).json()
    changed = call("PATCH", "/me/contacts/" + cids["Ann"], 200, headers={"If-Match": cont["@odata.etag"]}, json={"jobTitle": "Engineer"}).json()
    check(changed["jobTitle"] == "Engineer" and changed["mobilePhone"] == "+41 79 000 00 00", "contact patch")

    # Delete what was made; the lists no longer show it.
    for path in reversed(created):
        call("DELETE", path, 204)
    created.clear()
    left = call("GET", "/me/events", 200, params={"$filter": "contains(subject,'%s')" % TAG}).json()["value"]
    check(left == [], "events left behind")
    print("graph script ok")


try:
    main()
finally:
    cleanup()
