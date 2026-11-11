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
  (such as the Slack desktop app) has slack://, and it never changes an app
  you picked with Always. To make Clack that default anyway, tick the box in
  the installer or run:

    Clack.exe -register-url-handler

  To remove Clack's registration (other apps' stay as they are):

    Clack.exe -unregister-url-handler

  Uninstalling removes Clack's keys too.

SmartScreen
  Clack is not code-signed, so Windows may say it "protected your PC".
  Click "More info", then "Run anyway".

Clack is not affiliated with or endorsed by Slack Technologies.
