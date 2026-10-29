# Set up alarm notifications

See [Alarms: Getting told](../features/alarms.md#getting-told) for what each
transport is and what it needs. These are the steps for turning one on and
checking that it works.

1. Open **Settings → Alarms**. If you're already on the Alarms page, the gear
   icon in the header (on a tablet or larger) takes you straight there instead.
2. Turn on the transport you want under **Notifications**:
   - **ntfy**: set **Server** (the public `https://ntfy.sh` or your own) and
     **Topic**. If your server needs one, paste in an **Access token** too.
   - **Email**: set the SMTP **Server**, **Port**, **From** and **To**
     addresses. If the server requires authentication, set a **Username** and
     **Password** as well.
   - **Webhook**: set the **URL** to post to.
   - **Publish to SignalK**: there are no fields to fill in. It uses the
     instrument-network connection that's already set up for the boat.
   - **Web push (browser & phone)**: read [Web push over
     Tailscale](../reference/configuration.md#web-push-over-tailscale) first.
     Helmcentral has to be served over https before the toggle does anything.
   - **Address for links**: optional. The address you reach Helmcentral on from
     your phone, for example `http://boat.local:9091`. Notifications about a
     watch use it to link to the report. Leave it empty and they carry no link.
3. Once you've changed something, choose **Save** in the bar that appears
   across the top. **Discard** puts everything back as it was.
4. Choose **Send Test** at the top of the Notifications panel. It sends a
   sample alert through every transport you've enabled, so you can confirm
   delivery before you rely on it at sea.

If you change an ntfy server or SMTP host afterwards, you'll need to re-enter
that transport's token or password. Changing the destination clears the
credential that belonged to it, and the transport stays quiet until you paste
it back in. Changing an SMTP username clears its password for the same reason.
