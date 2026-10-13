package containerspec

import (
	"encoding/base64"
	"fmt"
)

// Configuration is created through the live source mount with its owner's UID.
// Database credentials remain environment references, never plaintext in source.
const wordpressInit = `<?php
$path = '/var/www/html/wp-config.php';
if (is_file($path)) { exit(0); }
foreach (['WORDPRESS_DB_HOST', 'WORDPRESS_DB_NAME', 'WORDPRESS_DB_USER'] as $key) {
    if (!getenv($key)) { fwrite(STDERR, "Missing WordPress database environment: $key\n"); exit(1); }
}
$user = getenv('APACHE_RUN_USER') ?: 'www-data';
$group = getenv('APACHE_RUN_GROUP') ?: 'www-data';
$uid = str_starts_with($user, '#') ? intval(substr($user, 1)) : posix_getpwnam($user)['uid'];
$gid = str_starts_with($group, '#') ? intval(substr($group, 1)) : posix_getgrnam($group)['gid'];
if (!posix_setgid($gid) || !posix_setuid($uid)) { fwrite(STDERR, "Cannot adopt source UID/GID\n"); exit(1); }
$config = "<?php\n";
foreach (['HOST', 'NAME', 'USER', 'PASSWORD'] as $key) {
    $config .= "define('DB_$key', getenv('WORDPRESS_DB_$key') ?: '');\n";
}
$config .= "define('DB_CHARSET', 'utf8');\ndefine('DB_COLLATE', '');\n";
foreach (['AUTH_KEY', 'SECURE_AUTH_KEY', 'LOGGED_IN_KEY', 'NONCE_KEY', 'AUTH_SALT', 'SECURE_AUTH_SALT', 'LOGGED_IN_SALT', 'NONCE_SALT'] as $key) {
    $config .= "define('$key', '" . bin2hex(random_bytes(32)) . "');\n";
}
$config .= "if (isset(\$_SERVER['HTTP_X_FORWARDED_PROTO']) && \$_SERVER['HTTP_X_FORWARDED_PROTO'] === 'https') { \$_SERVER['HTTPS'] = 'on'; }\n";
$config .= "\$table_prefix = 'wp_';\ndefine('ABSPATH', __DIR__ . '/');\nrequire_once ABSPATH . 'wp-settings.php';\n";
// x mode avoids replacing a configuration written concurrently by the user.
$file = @fopen($path, 'x');
if (!$file) { if (is_file($path)) { exit(0); } fwrite(STDERR, "Source directory is not writable by the application UID/GID\n"); exit(1); }
if (fwrite($file, $config) !== strlen($config)) { fclose($file); unlink($path); exit(1); }
fclose($file);
chmod($path, 0640);
`

func wordpressEntrypoint() string {
	script := "#!/bin/sh\nset -e\nphp /usr/local/bin/devbox-wordpress-init.php\nexec \"$@\"\n"
	return fmt.Sprintf("RUN php -r 'exit(extension_loaded(\"posix\") ? 0 : 1);' || docker-php-ext-install posix\nRUN printf '%%s' '%s' | base64 -d > /usr/local/bin/devbox-wordpress-init.php && printf '%%s' '%s' | base64 -d > /usr/local/bin/devbox-wordpress-entrypoint && chmod 755 /usr/local/bin/devbox-wordpress-entrypoint\nENTRYPOINT [\"/usr/local/bin/devbox-wordpress-entrypoint\"]\n", base64.StdEncoding.EncodeToString([]byte(wordpressInit)), base64.StdEncoding.EncodeToString([]byte(script)))
}
