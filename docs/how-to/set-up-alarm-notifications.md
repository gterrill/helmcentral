# Set up alarm notifications

See [Alarms: Getting told](../features/alarms.md#getting-told) for what each
transport is and what it needs. These are the steps for turning one on and
checking it works.

1. Open **Settings → Alarms**.
2. Turn on the transport you want under **Notifications**:
   - **ntfy**: set **Server** (the public `https://ntfy.sh` or your own) and
     **Topic**, and paste in an **Access token** if your server needs one.
   - **Email**: set the SMTP **Server**, **Port**, **From** and **To**
     addresses, and a **Username** and **Password** if the server requires
     authentication.
   - **Webhook**: set the **URL** to post to.
   - **Publish to SignalK**: no fields; it uses the instrument-network
     connection already configured for the boat.
   - **Web push (browser & phone)**: see [Web push over
     Tailscale](../reference/configuration.md#web-push-over-tailscale) first;
     it needs Helmcentral served over https before the toggle does anything.
3. Save Settings.
4. Choose **Send Test** at the top of the Notifications panel. It sends a
   sample alert through every transport you have enabled, so you can confirm
   delivery before relying on it at sea.

If you change an ntfy server or SMTP host afterwards, re-enter that
transport's token or password: changing the destination clears the
credential that was bound to it, and the transport stays quiet until you
paste it back in. Changing an SMTP username clears its password for the same
reason.
