// Package uiatest drives the Windows shell's toast and Notification Center
// windows with UI Automation, for tests on the windows-applog job. It uses
// UIAutomationCore's IUIAutomation (COM), which sees the shell's XAML
// windows; .NET Framework's System.Windows.Automation, what Windows
// PowerShell loads, doesn't list them. Nothing outside tests imports it.
package uiatest
