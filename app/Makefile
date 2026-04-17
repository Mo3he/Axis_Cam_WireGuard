PROGS = wireguardconfig
SRCS  = config_updater.c
OBJS  = $(SRCS:.c=.o)
STRIP ?= strip

PKGS = glib-2.0 gio-2.0 axparameter

CFLAGS += -Wall \
          -Wextra \
          -Wstrict-prototypes \
          -Wmissing-prototypes \
          -W \
          -Werror
CFLAGS += $(shell PKG_CONFIG_PATH=$(PKG_CONFIG_PATH) pkg-config --cflags $(PKGS))
LIBS   += $(shell PKG_CONFIG_PATH=$(PKG_CONFIG_PATH) pkg-config --libs $(PKGS))

all: $(PROGS)
	$(STRIP) $(PROGS)

$(PROGS): $(OBJS)
	$(CC) $(LDFLAGS) $^ $(LIBS) -o $@

clean:
	rm -f $(PROGS) *.o *.eap* *_LICENSE.txt package.conf* param.conf tmp*
	rm -f lib/wireguard-userspace
