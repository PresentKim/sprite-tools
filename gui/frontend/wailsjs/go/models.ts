export namespace main {
	
	export class FileEntry {
	    path: string;
	    name: string;
	    dir: string;
	    root: string;
	    width: number;
	    height: number;
	    bytes: number;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new FileEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.name = source["name"];
	        this.dir = source["dir"];
	        this.root = source["root"];
	        this.width = source["width"];
	        this.height = source["height"];
	        this.bytes = source["bytes"];
	        this.error = source["error"];
	    }
	}
	export class InputFile {
	    path: string;
	    root: string;
	
	    static createFrom(source: any = {}) {
	        return new InputFile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.root = source["root"];
	    }
	}
	export class JobRequest {
	    command: string;
	    threshold: number;
	    maxColors: number;
	    scale: number;
	    frameSize: string;
	    outDir: string;
	    keepEmpty: boolean;
	    palette: string;
	    suffix: string;
	    inPlace: boolean;
	    threads: number;
	    pad: number;
	    saveName: string;
	    force: boolean;
	    cols: number;
	    gap: number;
	    align: string;
	
	    static createFrom(source: any = {}) {
	        return new JobRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.command = source["command"];
	        this.threshold = source["threshold"];
	        this.maxColors = source["maxColors"];
	        this.scale = source["scale"];
	        this.frameSize = source["frameSize"];
	        this.outDir = source["outDir"];
	        this.keepEmpty = source["keepEmpty"];
	        this.palette = source["palette"];
	        this.suffix = source["suffix"];
	        this.inPlace = source["inPlace"];
	        this.threads = source["threads"];
	        this.pad = source["pad"];
	        this.saveName = source["saveName"];
	        this.force = source["force"];
	        this.cols = source["cols"];
	        this.gap = source["gap"];
	        this.align = source["align"];
	    }
	}
	export class PackResult {
	    canceled: boolean;
	    output: string;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new PackResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.canceled = source["canceled"];
	        this.output = source["output"];
	        this.message = source["message"];
	    }
	}
	export class PaletteInfo {
	    name: string;
	    path: string;
	    colors: string[];
	
	    static createFrom(source: any = {}) {
	        return new PaletteInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	        this.colors = source["colors"];
	    }
	}
	export class PreviewResult {
	    status: string;
	    message: string;
	    image?: string;
	    width: number;
	    height: number;
	    colors?: string[];
	    millis: number;
	
	    static createFrom(source: any = {}) {
	        return new PreviewResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = source["status"];
	        this.message = source["message"];
	        this.image = source["image"];
	        this.width = source["width"];
	        this.height = source["height"];
	        this.colors = source["colors"];
	        this.millis = source["millis"];
	    }
	}

}

