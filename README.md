# cfs-binary-release

cfs-binary-release is how we perform vendoring AND release management.

###

If you're using not using Go 1.6 or great you have to set GO15VENDOREXPERIMENT=1

### Updating a existing binary for release

1. Run `make yourbinaryname` to copy the target to the binaries/ directory
2. Run `glide get github.com/your/new/package`
3. Run `make update` or `glide up` to update packages
4. Edit VERSION and increment it
5. Git add any new things
6. Send Pull request

### Updating dependencies of existing binaries for release

1. go get the dependency to your system like usual (assuming its an external one)
2. Run `make $THE_CFS_BINARY_NAME` to update the binary (i.e. make formic)
3. Run `make update`
4. Run `make test`
5. Edit VERSION and increment it
6. git add any new things
7. Send Pull request

### Updating a *specific* dependency

1. go get -u the dependency to your system like usual
2. run `glide get the/dependency/thing`
3. run `glide up the/dependency/thing` & `glide install`
4. Optionally run `make test`
5. Edit VERSION and increment it
6. git add any new things
7. Send Pull request

### Adding a new binary for release

1. Edit the Makefile and add a new "yourbinaryname" target to copy the binary to binaries/yourbinaryname
2. Edit the Makefile and add a new "yourbinaryname" build target to build the binary in builds/
3. Edit the Makefile and add your new target to the `sync-all` targets dependencies 
4. Profit? 

### Do all the things!

1. Run `make world`, sync's all existing binaries, and updates ALL dependencies

### Restore

1. Edit glide.yaml version
2. glide install 
3. /vendor is now what you specified

### Beware the .gitignore!

The .gitignore in binaries/ explicitly only allows files of type `.go, .conf, and .toml` to make sure the repo stays clean and doesn't accidentally get contaminated with other cruft.
