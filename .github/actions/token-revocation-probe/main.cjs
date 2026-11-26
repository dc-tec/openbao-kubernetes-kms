'use strict';
try {
  require('./probe.cjs').register();
} catch {
  console.error('Could not register App token cleanup probe');
  process.exitCode = 1;
}
