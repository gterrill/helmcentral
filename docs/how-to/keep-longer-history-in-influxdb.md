# Keep a longer history in InfluxDB

InfluxDB keeps every reading SignalK logs for as long as its bucket's retention
allows, and then deletes it. A month is a common setting. That's often too
short. A slow fault such as a fuel pickup clogging can take a week to show and
another week to diagnose. By then the readings from when it started are nearly
gone.

Keeping every per-second reading for a year takes a lot of disk. These steps
keep the full readings for about three months, and a second bucket of
one-minute averages for one to two years. A one-minute average of a reading
sent every second takes about a sixtieth of the room. It's still detailed
enough to compare an engine's coolant or oil pressure at the same revs months
apart.

Going from 30 to 90 days of full readings roughly triples the room the main
bucket takes. Look at how big InfluxDB's data folder is now, and check the
machine has that much free twice over, before you change it.

You need:

- **InfluxDB 2:** The one Helmcentral already reads, set up under
  **Settings → InfluxDB**.
- **Its web page:** Usually port 8086 on the machine that runs InfluxDB, for
  example `http://192.168.1.10:8086`.
- **Your bucket's name:** The one shown in **Settings → InfluxDB**. These steps
  call it `SignalK_Data`. Use your own name wherever that appears.

Helmcentral reads only the bucket named in its settings. The one-minute bucket
is a record you can query yourself in InfluxDB's **Data Explorer**.

## Keep the full readings for 90 days

1. In InfluxDB, open **Load Data → Buckets**.
2. On your SignalK bucket, click **Settings**.
3. Under **Delete Data**, choose **Older than** and set it to 90 days.
4. Click **Save Changes**.

A longer retention only counts from now on. Readings already deleted don't
come back.

## Make the one-minute bucket

1. In **Load Data → Buckets**, click **Create Bucket**.
2. Name it `SignalK_1m`.
3. Under **Delete Data**, choose **Older than** and set it to 730 days, which
   is two years.
4. Click **Create**.

## Fill it every hour

A task is a query InfluxDB runs on a schedule. This one averages the last
hour's readings into one-minute values and writes them to the new bucket.

1. Open **Tasks** and click **Create Task**.
2. Name it `SignalK_1m downsample`.
3. Set **Every** to `1h` and **Offset** to `5m`. The offset gives late readings
   five minutes to arrive before the hour is averaged.
4. Paste this as the script, with your bucket's name in the first line:

   ```flux
   import "types"

   from(bucket: "SignalK_Data")
     |> range(start: -1h)
     |> filter(fn: (r) => r._field == "value")
     |> filter(fn: (r) => r._measurement =~ /^(propulsion\.|electrical\.|environment\.|tanks\.|navigation\.speedOverGround$)/)
     |> filter(fn: (r) => types.isType(v: r._value, type: "float"))
     |> aggregateWindow(every: 1m, fn: mean, createEmpty: false, timeSrc: "_start")
     |> to(bucket: "SignalK_1m")
   ```

5. Click **Save**.

What each part does:

- **Which readings:** The second filter keeps the engines, the electrical
  system, the weather instruments, the tanks and speed over ground. It leaves
  out radar and the rest. If you want everything, delete that line.
- **Numbers only:** SignalK also logs words and on/off flags, such as an
  engine's state or a charger's mode, under the same name as the numbers. An
  average of those makes no sense, and InfluxDB stops the whole task if it
  tries. The third filter keeps only the numbers.
- **Time stamps:** Each average is stamped with the start of its minute, so it
  lines up with the full readings it came from.
- **Senders kept apart:** Each sender of a reading stays separate, so two
  sensors reporting the same thing aren't averaged together.

Don't put an `option task` line in the script. The task form writes that
itself from the **Name**, **Every** and **Offset** fields, and the form
refuses a script that refers to `task.every`.

## Copy in what's already there

The task only averages the hour just gone. To keep what the main bucket still
has, run the same query once by hand:

1. Open **Data Explorer** and switch to **Script Editor**.
2. Paste the script from the previous section, and change `range(start: -1h)`
   to `range(start: -30d)`. Use your old retention in place of 30 days.
3. Click **Submit**.

If the query times out, run it a week at a time instead, with dates that start
and stop on midnight. For example,
`range(start: 2026-10-12T00:00:00Z, stop: 2026-10-19T00:00:00Z)`, then the next
week, and so on up to now. A stop time in the middle of a minute would leave
that minute's average built from only part of its readings.

## Tell Helmcentral about it

Helmcentral learns the usual readings of each engine and of the house bank from
one-minute averages, for the
[unusual engine readings](../features/anomaly-detection.md#unusual-engine-readings)
and [unusual house bank readings](../features/anomaly-detection.md#unusual-house-bank-readings)
suggestions. You don't have to name the bucket. Left blank, it works the minutes
out from the main bucket. To read the one-minute bucket instead, which is quicker
and reaches back further:

1. Open **Settings → InfluxDB**.
2. In **One-minute bucket (optional)**, type the bucket's name, for example
   `SignalK_1m`.
3. Choose **Save** in the bar across the top of the screen.

If Helmcentral can't read that bucket, it says so under **Learned normal** and
carries on with what it learned before.

## Check that it works

1. After the first hour, open **Tasks**, click the task and look at its runs.
   Each one should say **success**.
2. In **Data Explorer**, pick the `SignalK_1m` bucket and an engine reading
   such as `propulsion.port.revolutions`. You should see one point a minute
   while the engine ran.

If a run fails, the task fails the same way every hour, without telling
anyone. Its run log shows the error.
