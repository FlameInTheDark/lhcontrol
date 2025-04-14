export namespace main {
	
	export class StationInfo {
	    name: string;
	    address: string;
	    powerState: number;
	
	    static createFrom(source: any = {}) {
	        return new StationInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.address = source["address"];
	        this.powerState = source["powerState"];
	    }
	}

}

