#!/usr/bin/env bash
# Seeds a fresh classic instance over its DAV API and occ, then writes the
# database dump and its description (dumps/*.sql and dumps/*.json).
#
#   CLASSIC_DB=mysql (default) or pgsql
#
# The instance must be new: run `docker compose -f test/classic/compose.yml
# down -v` first when seeding again. See README.md.
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
db=${CLASSIC_DB:-mysql}
events=${CLASSIC_EVENTS:-5000}
jobs=${CLASSIC_JOBS:-8}

case $db in
mysql)
  svc=classic
  dbsvc=mariadb
  port=${CLASSIC_PORT:-18080}
  dumpfile=$here/dumps/mariadb.sql
  ;;
pgsql)
  svc=classic-pgsql
  dbsvc=postgres
  port=${CLASSIC_PG_PORT:-18081}
  dumpfile=$here/dumps/postgres.sql
  ;;
*)
  echo "CLASSIC_DB must be mysql or pgsql" >&2
  exit 2
  ;;
esac

project=classic-fixture
occ_c=$project-$svc-1
db_c=$project-$dbsvc-1
base=http://localhost:$port
dav=$base/remote.php/dav
secret=fixture-secret-9f3b1c7a5e2d4f60
tables="oc_users oc_accounts oc_groups oc_group_user oc_calendars oc_calendarobjects oc_calendarchanges oc_calendarsubscriptions oc_schedulingobjects oc_dav_shares oc_dav_properties oc_addressbooks oc_cards oc_cards_properties oc_addressbookchanges oc_authtoken"

occ() { docker exec "$occ_c" occ "$@"; }
sql() {
  if [ "$db" = mysql ]; then
    docker exec -i "$db_c" mariadb -N -B -uowncloud -powncloud owncloud -e "$1"
  else
    docker exec -i "$db_c" psql -t -A -U owncloud owncloud -c "$1"
  fi
}

echo "waiting for $base"
for _ in $(seq 120); do
  if curl -fs "$base/status.php" | grep -q '"installed":true'; then break; fi
  sleep 2
done
curl -fs "$base/status.php" | grep -q '"installed":true' || { echo "instance did not come up" >&2; exit 1; }
version=$(curl -fs "$base/status.php" | sed -E 's/.*"versionstring":"([^"]+)".*/\1/')

if occ user:list | grep -q -- '- alice:'; then
  echo "alice already exists; start from a fresh instance (compose down -v)" >&2
  exit 1
fi
# the image installs calendar and contacts after the first start
for _ in $(seq 90); do
  apps=$(occ app:list 2>/dev/null || true)
  if grep -q -- '- calendar' <<<"$apps" && grep -q -- '- contacts' <<<"$apps"; then break; fi
  sleep 2
done
grep -q -- '- calendar' <<<"$apps" && grep -q -- '- contacts' <<<"$apps" ||
  { echo "calendar and contacts apps are missing" >&2; exit 1; }

# The token hash is sha512(token . secret), so fix the secret first.
occ config:system:set secret --value "$secret" >/dev/null

# users and groups
for u in alice bob carol; do
  docker exec "$occ_c" bash -c "OC_PASS=$u-pass occ user:add --password-from-env --display-name=$u $u" >/dev/null
  occ user:modify "$u" email "$u@example.org" >/dev/null
done
occ group:add team >/dev/null
occ group:add staff >/dev/null
for u in bob carol; do occ group:add-member -m "$u" team >/dev/null; done
for u in alice bob; do occ group:add-member -m "$u" staff >/dev/null; done

req() { # user method path [curl args...]; fails on any status of 400 or above
  local user=$1 method=$2 path=$3
  shift 3
  curl -fsS -o /dev/null -u "$user:$user-pass" -X "$method" "$dav/$path" "$@"
}

mkcal() { # user uri displayname color
  req "$1" MKCALENDAR "calendars/$1/$2" -H 'Content-Type: application/xml' --data "<c:mkcalendar xmlns:c=\"urn:ietf:params:xml:ns:caldav\" xmlns:d=\"DAV:\" xmlns:a=\"http://apple.com/ns/ical/\"><d:set><d:prop><d:displayname>$3</d:displayname><a:calendar-color>$4</a:calendar-color><c:supported-calendar-component-set><c:comp name=\"VEVENT\"/></c:supported-calendar-component-set></d:prop></d:set></c:mkcalendar>"
}

share() { # user path "principal:principals/users/bob[ rw]" ...
  local user=$1 path=$2 set="" entry
  shift 2
  for entry in "$@"; do
    case $entry in
    *" rw") set="$set<oc:set><d:href>${entry% rw}</d:href><oc:read-write/></oc:set>" ;;
    *) set="$set<oc:set><d:href>$entry</d:href></oc:set>" ;;
    esac
  done
  req "$user" POST "$path" -H 'Content-Type: application/xml' --data "<?xml version=\"1.0\"?><oc:share xmlns:oc=\"http://owncloud.org/ns\" xmlns:d=\"DAV:\">$set</oc:share>"
}

ics() { # uid summary extra-lines...
  local uid=$1 summary=$2
  shift 2
  printf 'BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//classic fixture//EN\r\nBEGIN:VEVENT\r\nUID:%s\r\nDTSTAMP:20260101T000000Z\r\nSUMMARY:%s\r\n' "$uid" "$summary"
  local l
  for l in "$@"; do printf '%s\r\n' "$l"; done
  printf 'END:VEVENT\r\nEND:VCALENDAR\r\n'
}

put() { # user calendar uid summary extra-lines...
  local user=$1 cal=$2 uid=$3
  shift 3
  ics "$uid" "$@" | req "$user" PUT "calendars/$user/$cal/$uid.ics" -H 'Content-Type: text/calendar' --data-binary @-
}

# One event of the big calendar. The mix covers plain, all-day, recurring with
# an excluded date, reminder, an external attendee (a local one would make
# the server deliver copies to that user), escaped text and non-ASCII text.
bulk_event() {
  local i=$1 n m d h summary
  n=$(printf '%05d' "$i")
  m=$(printf '%02d' $((1 + i % 12)))
  d=$(printf '%02d' $((1 + i % 28)))
  h=$(printf '%02d' $((8 + i % 9)))
  summary="Event $n"
  local -a extra
  if ((i % 100 == 0)); then
    extra=("DTSTART;VALUE=DATE:2026${m}${d}" "DTEND;VALUE=DATE:2026${m}$(printf '%02d' $((1 + i % 28 + 1)))")
  elif ((i % 50 == 0)); then
    extra=("DTSTART:20260105T090000Z" "DTEND:20260105T100000Z" "RRULE:FREQ=WEEKLY;COUNT=10" "EXDATE:20260119T090000Z")
  else
    extra=("DTSTART:2026${m}${d}T${h}0000Z" "DTEND:2026${m}${d}T$(printf '%02d' $((9 + i % 9)))0000Z")
  fi
  if ((i % 7 == 0)); then summary="Besprechung $n Café ü"; fi
  if ((i % 11 == 0)); then
    extra+=("LOCATION:Room $((i % 40))\\, Building A" "DESCRIPTION:Line one\\nLine two\\, with a comma" "CATEGORIES:team,planning")
  fi
  if ((i % 25 == 0)); then
    extra+=("ORGANIZER:mailto:alice@example.org" "ATTENDEE;CN=Outside;PARTSTAT=ACCEPTED:mailto:outside@example.net")
  fi
  if ((i % 250 == 0)); then
    extra+=("BEGIN:VALARM" "ACTION:DISPLAY" "DESCRIPTION:reminder" "TRIGGER:-PT15M" "END:VALARM")
  fi
  put alice work "evt-$n" "$summary" "${extra[@]}"
}
export -f bulk_event put ics req
export dav

# calendars
mkcal alice work Work '#1d2d44'
mkcal bob projects Projects '#c1121f'
mkcal alice archive 'Archiv 2024 (ä)' '#6a994e'

echo "creating $events events in alice/work"
export occ_c
seq 1 "$events" | xargs -P "$jobs" -I{} bash -c 'bulk_event {}'

# alice/personal exists by default. Time zone, override of one occurrence,
# multi-day all-day event.
req alice PUT calendars/alice/personal/tz-meeting.ics -H 'Content-Type: text/calendar' --data-binary @- <<'ICS'
BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//classic fixture//EN
BEGIN:VTIMEZONE
TZID:Europe/Prague
BEGIN:STANDARD
DTSTART:19701025T030000
TZOFFSETFROM:+0200
TZOFFSETTO:+0100
RRULE:FREQ=YEARLY;BYMONTH=10;BYDAY=-1SU
END:STANDARD
BEGIN:DAYLIGHT
DTSTART:19700329T020000
TZOFFSETFROM:+0100
TZOFFSETTO:+0200
RRULE:FREQ=YEARLY;BYMONTH=3;BYDAY=-1SU
END:DAYLIGHT
END:VTIMEZONE
BEGIN:VEVENT
UID:tz-meeting
DTSTAMP:20260101T000000Z
DTSTART;TZID=Europe/Prague:20260302T140000
DTEND;TZID=Europe/Prague:20260302T150000
SUMMARY:Standup
RRULE:FREQ=DAILY;COUNT=5;BYDAY=MO,TU,WE,TH,FR
END:VEVENT
BEGIN:VEVENT
UID:tz-meeting
DTSTAMP:20260101T000000Z
RECURRENCE-ID;TZID=Europe/Prague:20260304T140000
DTSTART;TZID=Europe/Prague:20260304T160000
DTEND;TZID=Europe/Prague:20260304T170000
SUMMARY:Standup (moved)
END:VEVENT
END:VCALENDAR
ICS
put alice personal holiday "Summer holiday" "DTSTART;VALUE=DATE:20260801" "DTEND;VALUE=DATE:20260815" "TRANSP:TRANSPARENT"
put alice personal dentist "Dentist" "DTSTART:20260210T090000Z" "DTEND:20260210T094500Z" "CLASS:PRIVATE" "STATUS:TENTATIVE"
for i in 1 2 3; do
  put bob projects "kickoff-$i" "Project step $i" "DTSTART:2026060${i}T100000Z" "DTEND:2026060${i}T110000Z"
done

for i in 1 2; do
  put alice archive "old-$i" "Alter Termin $i" "DTSTART:2024030${i}T100000Z" "DTEND:2024030${i}T110000Z"
done

# shares: user, group, read-only and read-write, both directions
share alice calendars/alice/work "principal:principals/users/bob rw" "principal:principals/groups/team"
share alice calendars/alice/personal "principal:principals/users/carol"
share bob calendars/bob/projects "principal:principals/users/alice rw"

# a public link for alice/work
req alice POST calendars/alice/work -H 'Content-Type: application/xml' \
  --data '<?xml version="1.0"?><cs:publish-calendar xmlns:cs="http://calendarserver.org/ns/"/>'

# subscriptions; the sources do not resolve, so nothing is fetched
for u in alice bob; do
  req "$u" MKCOL "calendars/$u/holidays" -H 'Content-Type: application/xml' --data '<?xml version="1.0"?><d:mkcol xmlns:d="DAV:" xmlns:cs="http://calendarserver.org/ns/"><d:set><d:prop><d:resourcetype><d:collection/><cs:subscribed/></d:resourcetype><d:displayname>Holidays</d:displayname><cs:source><d:href>https://calendar.invalid/holidays.ics</d:href></cs:source></d:prop></d:set></d:mkcol>'
done

# contacts
card() { # user book n
  local user=$1 book=$2 n=$3 num name extra=""
  num=$(printf '%03d' "$n")
  name="Contact$num"
  if ((n % 10 == 0)); then extra=$'BDAY:1985-0'$((1 + n % 9))$'-1'$((n % 9))$'\r\nORG:Example '$num$';Sales\r\nNOTE:Met at the fair\\, 2025\\nCall back\r\n'; fi
  if ((n % 15 == 0)); then name="Zoë Ünal$num"; fi
  printf 'BEGIN:VCARD\r\nVERSION:3.0\r\nUID:card-%s-%s-%s\r\nFN:%s Example\r\nN:Example;%s;;;\r\nEMAIL;TYPE=WORK:c%s@example.org\r\nTEL;TYPE=CELL:+4930%06d\r\n%sEND:VCARD\r\n' \
    "$user" "$book" "$num" "$name" "$name" "$num" "$n" "$extra" |
    req "$user" PUT "addressbooks/users/$user/$book/card-$num.vcf" -H 'Content-Type: text/vcard' --data-binary @-
}
for n in $(seq 1 120); do card alice contacts "$n"; done
req alice MKCOL addressbooks/users/alice/friends -H 'Content-Type: application/xml' --data '<?xml version="1.0"?><d:mkcol xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:carddav"><d:set><d:prop><d:resourcetype><d:collection/><c:addressbook/></d:resourcetype><d:displayname>Friends</d:displayname></d:prop></d:set></d:mkcol>'
for n in $(seq 1 15); do card alice friends "$n"; done
for n in $(seq 1 10); do card bob contacts "$n"; done
share alice addressbooks/users/alice/friends "principal:principals/users/bob rw" "principal:principals/groups/team"
share bob addressbooks/users/bob/contacts "principal:principals/users/alice"

# app tokens, created the way the personal security page does: log in with a
# session, then ask for a token with a device name.
tokens=""
app_token() { # user device
  local user=$1 device=$2 jar rt out token
  jar=$(mktemp)
  rt=$(curl -fs -c "$jar" -b "$jar" "$base/index.php/login" | grep -o 'data-requesttoken="[^"]*"' | head -1 | cut -d'"' -f2)
  curl -fs -c "$jar" -b "$jar" -o /dev/null --data-urlencode "user=$user" --data-urlencode "password=$user-pass" --data-urlencode "requesttoken=$rt" "$base/index.php/login"
  rt=$(curl -fs -c "$jar" -b "$jar" "$base/index.php/settings/personal" | grep -o 'data-requesttoken="[^"]*"' | head -1 | cut -d'"' -f2)
  out=$(curl -fs -c "$jar" -b "$jar" -X POST -H "requesttoken: $rt" --data-urlencode "name=$device" "$base/index.php/settings/personal/authtokens")
  rm -f "$jar"
  token=$(printf '%s' "$out" | sed -E 's/.*"token":"([^"]+)".*/\1/')
  digest=$(printf '%s%s' "$token" "$secret" | shasum -a 512 | cut -d' ' -f1)
  tokens="$tokens${tokens:+,}
    {\"user\": \"$user\", \"name\": \"$device\", \"token\": \"$token\", \"sha512\": \"$digest\"}"
}
app_token alice "Phone One"
app_token alice "DAVx5 on Pixel"
app_token bob "Thunderbird"

# drop the temporary session tokens the logins left behind
sql "DELETE FROM oc_authtoken WHERE type = 0" >/dev/null

# row counts, then the dump
rows=""
for t in $tables; do
  n=$(sql "SELECT count(*) FROM $t" | tr -d '[:space:]')
  rows="$rows${rows:+,}
    \"$t\": $n"
done
cals=""
for c in alice/work alice/personal alice/archive bob/projects; do
  o=${c%/*}
  n=$(curl -fs -u "$o:$o-pass" -X REPORT -H 'Depth: 1' -H 'Content-Type: application/xml' "$dav/calendars/$c/" \
    --data '<c:calendar-query xmlns:c="urn:ietf:params:xml:ns:caldav" xmlns:d="DAV:"><d:prop><d:getetag/></d:prop><c:filter><c:comp-filter name="VCALENDAR"><c:comp-filter name="VEVENT"/></c:comp-filter></c:filter></c:calendar-query>' | { grep -o '<d:getetag>' || true; } | wc -l | tr -d ' ')
  cals="$cals${cals:+,}
    \"$c\": $n"
done
# the server fills a birthday calendar per address book owner from the contacts
n=$(sql "SELECT count(*) FROM oc_calendarobjects WHERE calendarid IN (SELECT id FROM oc_calendars WHERE uri = 'contact_birthdays')" | tr -d '[:space:]')
cals="$cals,
    \"system/contact_birthdays\": $n"

mkdir -p "$here/dumps"
if [ "$db" = mysql ]; then
  docker exec "$db_c" mariadb-dump -uowncloud -powncloud --skip-extended-insert --skip-comments \
    --skip-dump-date --no-tablespaces --order-by-primary owncloud $tables >"$dumpfile"
else
  args=""
  for t in $tables; do args="$args -t $t"; done
  # shellcheck disable=SC2086
  docker exec "$db_c" pg_dump -U owncloud --inserts --no-owner --no-privileges --no-comments $args owncloud |
    grep -v -E '^\\(un)?restrict ' >"$dumpfile"
fi

# App tokens are random, so each database has its own description.
cat >"${dumpfile%.sql}.json" <<JSON
{
  "version": "$version",
  "secret": "$secret",
  "users": [
    {"name": "alice", "password": "alice-pass", "groups": ["staff"]},
    {"name": "bob", "password": "bob-pass", "groups": ["staff", "team"]},
    {"name": "carol", "password": "carol-pass", "groups": ["team"]}
  ],
  "app_tokens": [$tokens
  ],
  "rows": {$rows
  },
  "calendars": {$cals
  }
}
JSON
echo "wrote $dumpfile and ${dumpfile%.sql}.json"
