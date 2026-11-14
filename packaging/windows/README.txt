Clack - a native Slack client
https://github.com/some-du6e/clack

Run Clack.exe. On first start it asks for your workspace, token and cookie;
see "Sign in" in the project README.

Settings and the sign-in file live in %AppData%\clack.

slack:// links
  Signing in ends with your browser handing Clack a slack:// link. Every
  time Clack.exe starts, it registers itself for those links, for your user
  only (HKCU\Software\Classes\Clack.slack, HKCU\Software\Clack\Capabilities
  and HKCU\Software\RegisteredApplications). The installer writes the same
  keys. Windows then lists Clack when it asks which app should open a
  slack:// link ("Select an app to open this 'slack' link": pick Clack, then
  Just once or Always), and in Settings > Apps > Default apps.

  Clack only becomes the app Windows uses without asking when no other app
  (such as the Slack desktop app) has slack://. To make Clack that default
  anyway, tick "Make Clack the default for slack:// links" in the installer
  or run:

    Clack.exe -register-url-handler

  Neither changes an app you picked with Always in Windows' "Select an app"
  box: Windows protects that choice. Change it in Settings > Apps > Default
  apps > Choose defaults by link type (at the bottom of the page) > SLACK,
  and pick Clack. You can also type "slack" in that page's "Set a default
  for a file type or link type" box.

  To remove Clack's registration (other apps' stay as they are):

    Clack.exe -unregister-url-handler

  Uninstalling removes Clack's keys too.

Notifications
  Clack's notifications are Windows toasts, filed under Clack's app id,
  io.github.some-du6e.clack. Windows only shows toasts from an id it
  knows, so the installer registers it, and Clack.exe does too before its
  first toast. Both write the same key, for your user only:

    HKCU\Software\Classes\AppUserModelId\io.github.some-du6e.clack
      DisplayName  Clack
      IconUri      %LOCALAPPDATA%\Clack\clack.png

  Settings > System > Notifications then lists Clack, where you can turn
  its notifications off or change how they show. Clicking a toast opens
  the conversation while Clack is running. Clack registers no COM
  activator, so Windows can't start Clack from a toast it left in
  Notification Center. Uninstalling removes the key and the icon.

Notification area and taskbar
  While Clack runs, its icon sits in the notification area and shows
  whether you have unread messages or mentions; click it to bring Clack
  forward, right-click it for its menu. The menu's Launch Control starts
  Clack when you sign in to Windows (Launch on Login). Clack then stays in
  the notification area (Keep in Background, the default) unless you pick
  Show Clack Immediately. Right-clicking Clack's taskbar button lists your
  workspace and Move Clack to Current Display, which brings the window to
  the screen you're on.

SmartScreen
  Clack is not code-signed, so Windows may say it "protected your PC".
  Click "More info", then "Run anyway".

Clack is not affiliated with or endorsed by Slack Technologies.
