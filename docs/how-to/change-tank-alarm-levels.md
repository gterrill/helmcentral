# Change a tank's alarm levels

[Alarms](../features/alarms.md#tank-level-alarms) explains what the tank alarms
do. The levels you set here decide when a tank turns amber or red on the Tanks
tile and the Engine Cluster's fuel bars, and when its alarm is raised. They're
percentages of full.

## Change the levels for a kind of tank

Do this when every tank of a kind should use the same levels.

1. Open **Settings** and choose **Alarms**.
2. Find **Tank levels**. Fuel, Fresh water and Waste each have a row.
3. Set **Warn at** and **Alarm at** for the kind. For fuel and fresh water the
   alarm level must be lower than the warn level, because a low tank is the bad
   one. For waste it must be higher, because a full tank is the bad one.
4. Choose **Save** in the bar at the top.

Helmcentral tells you which kind is wrong and doesn't save until the pair runs
the right way round.

## Give one tank its own levels

Do this when one tank is different, for example a day tank that should warn
earlier.

1. Open **Inventory** and choose the tank's equipment item. It needs a SignalK
   source that is a tank. See [Set up your vessel](set-up-your-vessel.md).
2. Find **Tank levels**.
3. Type a **Warn at** or an **Alarm at**. The grey figure in each box is the
   default for that kind of tank. Leave a box empty to use it.
4. Choose **Save**.

## Switch a tank's alarms off

1. Open the tank's equipment item and find **Tank levels**.
2. Turn on **Alarms off**.
3. Choose **Save**.

The Tanks tile still shows the tank's level and colours. Only the alarm is
silenced. Turn **Alarms off** back off to get the alarm again.

## Things to know

- A tank with no equipment item isn't listed on the Tanks tile and never
  alarms. Give it an item first.
- The fuel bars on an Engine Cluster have no levels of their own. They follow
  the tank they show.
- A tank's own level can end up on the wrong side of a default you change later.
  If that happens, the tank raises no alarm until you fix it, and the log says
  why.
