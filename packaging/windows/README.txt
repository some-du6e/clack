Clack - a native Slack client
https://github.com/some-du6e/yoloslackgoclient

Run Clack.exe. On first start it asks for your workspace, token and cookie;
see "Sign in" in the project README.

Settings and the sign-in file live in %AppData%\clack.

slack:// links
  The installer registers Clack as the handler for slack:// links (you can
  untick that during setup). The portable zip does not touch the registry.
  To make the portable Clack.exe open slack:// links, run once:

    Clack.exe -register-url-handler

  and to undo it:

    Clack.exe -unregister-url-handler

  Both only change your own user's settings (HKCU\Software\Classes\slack).

SmartScreen
  Clack is not code-signed, so Windows may say it "protected your PC".
  Click "More info", then "Run anyway".

Clack is not affiliated with or endorsed by Slack Technologies.
