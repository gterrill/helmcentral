# Run your own Overpass server

Place names, Nearby, Mate's place lookups and the
[offline coastline](../features/offline-coastline.md) all ask an Overpass
server for OpenStreetMap data. The public ones are shared, and over a boat's link they are
slow when they answer at all. A server aboard answers in a fraction of a
second, never turns Mate away, keeps working when the link is down, and keeps
the boat's position off the internet.
[Place names and nearby places](../features/place-names.md#running-your-own-server)
weighs what it gets you against what it costs.

You need a home server that runs Docker apps (these steps use ZimaOS), a few
gigabytes of free disk (about 4 GB for the Queensland coast), and an internet
connection for the first import.

## Install the server

1. Get the app file:
   [docker-compose.yml](https://github.com/gterrill/helmcentral/blob/main/packaging/overpass/docker-compose.yml)
   (or the
   [raw file](https://raw.githubusercontent.com/gterrill/helmcentral/main/packaging/overpass/docker-compose.yml)
   to save). It is not part of the release download. Its header comment repeats
   these steps.
2. Choose your region and your cruising ground, and edit the two placeholders
   in the file. They are left blank on purpose, so an unedited file will not
   start.
   - `OVERPASS_PLANET_URL`: the download for the area that holds your cruising
     ground. Browse [download.geofabrik.de](https://download.geofabrik.de/),
     pick your country or state, and copy the link ending `-latest.osm.pbf`.
   - `--bbox WEST,SOUTH,EAST,NORTH`: the box to keep, in degrees. Make it as
     small as your cruising allows: a smaller box imports faster and takes less
     disk. Queensland, Gold Coast to Torres Strait, is
     `--bbox 142.0,-29.2,154.0,-9.0` with
     `australia-oceania/australia-latest.osm.pbf`.
3. On the home server, add the file as a custom app. On ZimaOS that is **App
   Store → Custom Install → Import**.
4. Start the app. The first start downloads the extract, crops it to your box
   and builds the database. Expect one to several hours. The server does not
   answer until that finishes.

The server does not update itself. To refresh it, or to change the box, delete
the app's `db` folder (`/DATA/AppData/overpass/db` on ZimaOS) and start it
again; it imports afresh. Place names change slowly, so every few months is
plenty.

## Point Helmcentral at it

1. Open **Settings → Plugins → OpenStreetMap (Overpass)**.
2. Set **Overpass server** to `http://<server-address>:12345/api/interpreter`,
   using the home server's address on the boat network, for example
   `http://192.168.1.20:12345/api/interpreter`.
3. Add the server address to **Allowed Hosts** (comma-separated), for example
   `192.168.1.20`. Saving that field replaces the whole list, so keep the
   entries already there, `en.wikipedia.org` in particular (it supplies the
   short descriptions), and keep the public servers too if you want them as a
   fallback you can switch back to.
4. Save, then restart Helmcentral. Changes to allowed hosts only take effect
   after a restart; the Overpass server field itself applies at once.

Plain http is accepted here because the server is on your own network. Any
address outside it has to be https.

## Check it

Open a Nearby map or ask Mate where the nearest anchorage is. If a lookup
fails, check three things: the import may still be running, you may not have
restarted Helmcentral since adding the address to Allowed Hosts, or the address
and port in **Overpass server** may not match the app. Once all three are right,
answers come back at once.
