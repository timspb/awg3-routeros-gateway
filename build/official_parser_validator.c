// SPDX-License-Identifier: GPL-2.0 OR MIT

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "config.h"
#include "containers.h"

static int validate_file(const char *path) {
	FILE *f = fopen(path, "r");
	if (!f) {
		perror("fopen");
		return 2;
	}

	struct config_ctx ctx;
	if (!config_read_init(&ctx, false)) {
		fclose(f);
		return 3;
	}

	char *line = NULL;
	size_t cap = 0;
	while (getline(&line, &cap, f) >= 0) {
		if (!config_read_line(&ctx, line)) {
			free(line);
			fclose(f);
			return 4;
		}
	}
	free(line);
	fclose(f);

	struct wgdevice *device = config_read_finish(&ctx);
	if (!device) {
		return 5;
	}
	free_wgdevice(device);
	return 0;
}

int main(int argc, char **argv) {
	if (argc != 2) {
		fprintf(stderr, "usage: %s <config>\n", argv[0]);
		return 1;
	}
	return validate_file(argv[1]);
}
