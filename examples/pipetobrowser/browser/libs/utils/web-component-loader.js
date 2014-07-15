export function importComponent(path) {
	return new Promise((resolve, reject) => {
		var link = document.createElement('link');
		link.setAttribute('rel', 'import');
		link.setAttribute('href', path);
		link.onload = function() {
		  resolve();
		};
		document.body.appendChild(link);
	});
}