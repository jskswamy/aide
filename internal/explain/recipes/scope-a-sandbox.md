# Scope a sandbox for a profile

If your config still uses the legacy `contexts:` key, put these entries
under `contexts:` instead (same fields); a file cannot mix the two.
`aide profile migrate` converts it.

The default sandbox protects SSH keys, cloud credentials, and browser
profiles. Add narrow extra paths rather than disabling it.

## Add writable or readable paths

    profiles:
      work:
        sandbox:
          writable_extra: ["~/work/cache"]
          readable_extra: ["/opt/work-tools"]

## Deny additional paths

    profiles:
      work:
        sandbox:
          denied_extra: ["~/.ssh"]

## Use a named sandbox profile

    profiles:
      work:
        sandbox: strict       # profile defined under sandboxes:

## Disable the sandbox (not recommended)

    profiles:
      work:
        sandbox: false
