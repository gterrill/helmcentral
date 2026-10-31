# Run your own Overpass server

Place names, Nearby, Mate's place lookups and the
[offline coastline](../features/offline-coastline.md) all ask an Overpass
server for OpenStreetMap data. The public ones are shared, and over a boat's
link they're slow when they answer at all. A server aboard answers in a
fraction of a second. It never turns Mate away, it keeps working when the link
is down, and it keeps the boat's position off the internet.
[Place names and nearby places](../features/place-names.md#running-your-own-server)
weighs the benefit against the cost.

You need three things:

- **A home server:** One that runs Docker apps (these steps use ZimaOS).
- **Disk space:** A few gigabytes free. The Queensland coast took about 4 GB
  before the server kept element timestamps, and takes more now. See [How much
  disk it needs](#how-much-disk-it-needs).
- **Internet:** A connection for the first import.

## Install the server

1. Get the app file:
   [docker-compose.yml](https://github.com/gterrill/helmcentral/blob/main/packaging/overpass/docker-compose.yml)
   (or the
   [raw file](https://raw.githubusercontent.com/gterrill/helmcentral/main/packaging/overpass/docker-compose.yml)
   to save). It isn't part of the release download. Its header comment repeats
   these steps.
2. Choose your region and your cruising ground, and edit the three
   placeholders in the file. They're blank on purpose, so an unedited file
   won't start.
   - `OVERPASS_PLANET_URL`: the download for the area that holds your cruising
     ground. Browse [download.geofabrik.de](https://download.geofabrik.de/),
     pick your country or state, and copy the link ending `-latest.osm.pbf`.
   - `OVERPASS_DIFF_URL`: the daily updates. It must be the `-updates/` feed of
     the same Geofabrik region as `OVERPASS_PLANET_URL`: the same address with
     `-updates/` in place of `-latest.osm.pbf`. For Queensland it is
     `https://download.geofabrik.de/australia-oceania/australia-updates/`. A
     feed for a different region would corrupt the database.
   - `--bbox WEST,SOUTH,EAST,NORTH`: the box to keep, in degrees. Make it as
     small as your cruising allows: a smaller box imports faster and takes less
     disk. Queensland, Gold Coast to Torres Strait, is
     `--bbox 142.0,-29.2,154.0,-9.0` with
     `australia-oceania/australia-latest.osm.pbf`.

   The file also keeps the server's element timestamps on and applies the daily
   updates. The weekly [offline
   coastline](../features/offline-coastline.md#keeping-it-current) check needs
   both. The timestamps let the server say what changed. The updates keep its
   map data current enough for there to be something to find.
3. On the home server, add the file as a custom app. On ZimaOS that is **App
   Store → Custom Install → Import**.
4. Start the app. The first start downloads the extract, crops it to your box
   and builds the database. Expect one to several hours. The server doesn't
   answer until that finishes.

Once it's running, the server applies the map's daily updates by itself, so
its data stays within a day or so of the public map. The updates also bring in
objects from outside your box, which only costs disk. To change the box, delete
the app's `db` folder (`/DATA/AppData/overpass/db` on ZimaOS) and start the app
again, and it imports afresh.

## How much disk it needs

Keeping element timestamps makes the database bigger than one without them.
The Overpass documentation says a database without them is the smallest kind.
It doesn't say by how much, and the only sizes it gives are for the whole
world. The Queensland coast took about 4 GB before timestamps were kept. Leave
room beyond that, and look at the size of the `db` folder once the import
finishes so you know your own figure. The daily updates grow it slowly after
that.

## Re-import a server set up before timestamps were kept

Re-import the server if the offline coastline page shows either of these under
the switch, after "Edit check on hold":

- **No timestamps:** "The Overpass server was set up without element
  timestamps, which the weekly edit check needs."
- **Stale data:** "The Overpass server behind the osm-overpass plugin has map
  data from ..., so it has stopped applying its daily updates and has nothing
  new to check."

A server installed before the weekly coastline check was added keeps no element
timestamps and applies no daily updates. It gives the first message, and the
second once it has timestamps. Changing the settings does nothing to the data already
imported. The server has to import again, with both set:

1. Update the app file. Either copy the current
   [docker-compose.yml](https://github.com/gterrill/helmcentral/blob/main/packaging/overpass/docker-compose.yml)
   again, keeping your own `OVERPASS_PLANET_URL` and `--bbox`, or edit the
   existing settings. Set `OVERPASS_META` to `"yes"`, and set
   `OVERPASS_DIFF_URL` to the `-updates/` feed of the same Geofabrik region as
   `OVERPASS_PLANET_URL`. Make sure `OVERPASS_UPDATE_SLEEP` is there too
   (`"86400"`, one day).
2. Stop the app, then delete the app's `db` folder (`/DATA/AppData/overpass/db`
   on ZimaOS).
3. Start the app. It imports again, which takes the same hours as the first
   import. The server doesn't answer until the import finishes, so lookups and
   the weekly check wait until then.

Check that the disk has room for the larger database before you start.

If the stale-data message comes back on a server you've already re-imported,
the server isn't applying its updates. Check that `OVERPASS_DIFF_URL` is set to
the right region's feed and that the app is running.

## Point Helmcentral at it

1. Open **Settings → Plugins → OpenStreetMap (Overpass)**.
2. Set **Overpass server** to `http://<server-address>:12345/api/interpreter`,
   using the home server's address on the boat network, for example
   `http://192.168.1.20:12345/api/interpreter`.
3. Add the server address to **Allowed Hosts** (comma-separated), for example
   `192.168.1.20`. Saving that field replaces the whole list, so keep the
   entries already there, `en.wikipedia.org` in particular (it supplies the
   short descriptions). Keep the public servers too if you want them as a
   fallback you can switch back to.
4. Save, then restart Helmcentral. Changes to allowed hosts only take effect
   after a restart. The Overpass server field itself applies at once.

Plain http is accepted here because the server is on your own network. Any
address outside it has to be https.

## Check it

Open a Nearby map or ask Mate where the nearest anchorage is. If a lookup
fails, check three things:

- **The import:** It may still be running.
- **The restart:** You may not have restarted Helmcentral since you added the
  address to Allowed Hosts.
- **The address:** The address and port in **Overpass server** may not match
  the app.

Once all three are right, answers come back at once.

To check that the daily updates are being applied, ask the server for its data
date the day after the import, and again a day later. In the answer to any
query, `timestamp_osm_base` under `osm3s` should have moved forward by about a
day. If it stays put, the updates aren't running.
