package guards

// TestDirExists exposes dirExists for testing.
var TestDirExists = dirExists

// TestPathExists exposes pathExists for testing.
var TestPathExists = pathExists

// TestNixStoreDir allows tests to override the nix store path without nix installed.
var TestNixStoreDir = &nixStoreDir
