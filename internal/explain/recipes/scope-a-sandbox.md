# Scope a sandbox for a context

The default sandbox protects SSH keys, cloud credentials, and browser
profiles. Add narrow extra paths rather than disabling it.

## Add writable or readable paths

    contexts:
      work:
        sandbox:
          writable_extra: ["~/work/cache"]
          readable_extra: ["/opt/work-tools"]

## Deny additional paths

    contexts:
      work:
        sandbox:
          denied_extra: ["~/.ssh"]

## Use a named sandbox profile

    contexts:
      work:
        sandbox: strict       # profile defined under sandboxes:

## Disable the sandbox (not recommended)

    contexts:
      work:
        sandbox: false
